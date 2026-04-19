package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service/memory"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// memoryHubPoster is the optional outbound sync target. When set
// (MEMORY_HUB_URL env), confirmed sharable memories are POSTed to
// {url}/api/v1/memories with a derived JSON body. Mirrors the
// MyMemo Memory Hub HTTP contract per reference doc §五.
//
// Phase R: pluggable cloud-sync. Default nil — listener stays
// log-only. Production sets MEMORY_HUB_URL to enable.
var (
	memoryHubURL    = os.Getenv("MEMORY_HUB_URL")
	memoryHubBearer = os.Getenv("MEMORY_HUB_TOKEN")
	memoryHubClient = &http.Client{Timeout: 10 * time.Second}
)

// postToMemoryHub fires a best-effort POST. Errors logged, never
// retried (caller bus is sync — retries belong in a dedicated worker).
func postToMemoryHub(ctx context.Context, eventType, workspaceID string, payload map[string]any) {
	if memoryHubURL == "" {
		return
	}
	body, err := json.Marshal(map[string]any{
		"event_type":   eventType,
		"workspace_id": workspaceID,
		"payload":      payload,
		"sent_at":      time.Now().UTC(),
	})
	if err != nil {
		slog.Warn("memory hub: marshal failed", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		memoryHubURL+"/api/v1/memories", bytes.NewReader(body))
	if err != nil {
		slog.Warn("memory hub: build request failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if memoryHubBearer != "" {
		req.Header.Set("Authorization", "Bearer "+memoryHubBearer)
	}
	resp, err := memoryHubClient.Do(req)
	if err != nil {
		slog.Warn("memory hub: post failed", "url", memoryHubURL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		slog.Warn("memory hub: non-2xx",
			"status", resp.StatusCode, "body", string(raw))
		return
	}
	slog.Info("memory hub: synced",
		"event_type", eventType,
		"workspace_id", workspaceID,
		"memory_id", payload["memory_id"])
}

// memorySyncEnabled is exposed for tests.
func memorySyncEnabled() bool { return memoryHubURL != "" }

// registerMemoryListeners wires bus subscribers for memory lifecycle
// events (memory.appended / memory.confirmed / memory.archived). Phase M
// of the memory plan: scope-based sync. Per the user-supplied
// reference doc §三 ("通过事件总线同步"), memory.confirmed is the
// gate that lets a non-private memory escalate to the cloud-shared
// surface — handlers wired here can promote / fan-out / audit without
// the writer (memory.Service.Promote) knowing about them.
//
// Current handlers:
//   memory.confirmed  →  scope-based sync log + analytics hook point
//   memory.archived   →  retention audit (placeholder)
//   memory.appended   →  no-op (audit-writer subscribes via SubscribeAll
//                        in registerActivityListeners; no scope work)
//
// All handlers are best-effort: errors are logged, never re-published.
// The Bus already recovers from panics per bus.go.
//
// hub is optional — pass nil to disable WS broadcasts (tests).
func registerMemoryListeners(bus *events.Bus, _ *db.Queries, hub *realtime.Hub) {
	bus.Subscribe(memory.EventMemoryConfirmed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		scope, _ := payload["scope"].(string)
		switch memory.MemoryScope(scope) {
		case memory.ScopePrivateLocal:
			// Per reference doc: private memories stay local. Skip
			// both log + WS broadcast — owner is the only audience.
			return
		case memory.ScopeSharedSummary,
			memory.ScopeTeam,
			memory.ScopeAgentState,
			memory.ScopeArchive:
			slog.Info("memory: confirmed for sharable scope",
				"workspace_id", e.WorkspaceID,
				"memory_id", payload["memory_id"],
				"type", payload["type"],
				"scope", scope,
				"raw_kind", payload["raw_kind"],
				"version", payload["version"],
			)
			// Phase P: WS broadcast so frontend can update without
			// poll. workspace-scoped — only members see their
			// confirmed memories.
			broadcastMemoryEvent(hub, "memory.confirmed", e.WorkspaceID, payload)
			// Phase R: pluggable cloud-sync. Async fire-and-forget
			// so bus doesn't block on a slow MyMemo Hub.
			go postToMemoryHub(context.Background(), "memory.confirmed", e.WorkspaceID, payload)
		default:
			slog.Warn("memory: confirmed with unknown scope",
				"workspace_id", e.WorkspaceID,
				"memory_id", payload["memory_id"],
				"scope", scope,
			)
		}
	})

	bus.Subscribe(memory.EventMemoryArchived, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		slog.Info("memory: archived",
			"workspace_id", e.WorkspaceID,
			"memory_id", payload["memory_id"],
			"type", payload["type"],
		)
		broadcastMemoryEvent(hub, "memory.archived", e.WorkspaceID, payload)
	})
}

// broadcastMemoryEvent fans the event out to every WS client in the
// workspace. Marshalling errors are logged but never propagate — WS
// is best-effort.
func broadcastMemoryEvent(hub *realtime.Hub, eventType, workspaceID string, payload map[string]any) {
	if hub == nil || workspaceID == "" {
		return
	}
	body, err := json.Marshal(map[string]any{
		"type":         eventType,
		"workspace_id": workspaceID,
		"payload":      payload,
	})
	if err != nil {
		slog.Warn("memory listener: marshal failed", "type", eventType, "err", err)
		return
	}
	hub.BroadcastToWorkspace(workspaceID, body)
}

package main

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// memoryHubURL / memoryHubBearer are the env-supplied target. Read once
// at process start. Empty url => cloud-sync disabled (poster Enqueue is
// a no-op). Per MyMemo Memory Hub HTTP contract (reference doc §五).
var (
	memoryHubURL    = os.Getenv("MEMORY_HUB_URL")
	memoryHubBearer = os.Getenv("MEMORY_HUB_TOKEN")
)

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
			// Phase R+T: cloud-sync via bounded worker pool. Drops
			// when queue full to keep bus latency bounded; never
			// blocks. Server lifecycle owns Start/Stop.
			defaultMemoryHubPoster.Enqueue("memory.confirmed", e.WorkspaceID, payload)
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

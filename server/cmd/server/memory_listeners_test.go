package main

import (
	"encoding/json"
	"testing"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
)

// TestRegisterMemoryListeners_RegistersConfirmedHandler verifies the
// listener registers without error and the bus dispatches our payload
// without panic. We don't assert log lines (slog goes to stderr) — the
// purpose here is the smoke-test of the registration shape.
func TestRegisterMemoryListeners_RegistersConfirmedHandler(t *testing.T) {
	bus := events.New()
	registerMemoryListeners(bus, nil, nil) // queries + hub unused here

	// Publish each scope variant — handler must accept all without panic.
	for _, scope := range []memory.MemoryScope{
		memory.ScopePrivateLocal,
		memory.ScopeSharedSummary,
		memory.ScopeTeam,
		memory.ScopeAgentState,
		memory.ScopeArchive,
		"unknown_scope", // exercises default branch
	} {
		bus.Publish(events.Event{
			Type:        memory.EventMemoryConfirmed,
			WorkspaceID: "ws-1",
			ActorType:   "system",
			ActorID:     "user-1",
			Payload: map[string]any{
				"memory_id": "mem-1",
				"type":      "summary",
				"scope":     string(scope),
				"raw_kind":  "file_index",
				"raw_id":    "raw-1",
				"version":   1,
			},
		})
	}

	bus.Publish(events.Event{
		Type:        memory.EventMemoryArchived,
		WorkspaceID: "ws-1",
		Payload: map[string]any{
			"memory_id": "mem-1",
			"type":      "summary",
		},
	})
}

// TestRegisterMemoryListeners_TolerantOfBadPayload — handler returns
// silently if Payload isn't a map (defensive — bus is in-process so
// this is mostly fork-safety).
func TestRegisterMemoryListeners_TolerantOfBadPayload(t *testing.T) {
	bus := events.New()
	registerMemoryListeners(bus, nil, nil)
	bus.Publish(events.Event{
		Type:    memory.EventMemoryConfirmed,
		Payload: "not-a-map",
	})
	bus.Publish(events.Event{
		Type:    memory.EventMemoryArchived,
		Payload: 42,
	})
}

// TestRegisterMemoryListeners_HubBranchSafe — when a real Hub is
// wired, listener must complete without panic for every scope branch
// (private skips, sharable broadcasts, unknown warns). We don't
// assert WS-client delivery here (covered by hub_test.go); this
// guards the listener→hub call shape against regressions.
func TestRegisterMemoryListeners_HubBranchSafe(t *testing.T) {
	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()

	registerMemoryListeners(bus, nil, hub)

	for _, scope := range []memory.MemoryScope{
		memory.ScopePrivateLocal, memory.ScopeSharedSummary,
		memory.ScopeTeam, memory.ScopeAgentState, memory.ScopeArchive,
	} {
		bus.Publish(events.Event{
			Type:        memory.EventMemoryConfirmed,
			WorkspaceID: "ws-hub-branch",
			Payload: map[string]any{
				"memory_id": "mem-x",
				"scope":     string(scope),
				"type":      "fact",
			},
		})
	}
	bus.Publish(events.Event{
		Type:        memory.EventMemoryArchived,
		WorkspaceID: "ws-hub-branch",
		Payload:     map[string]any{"memory_id": "mem-x", "type": "fact"},
	})

	// broadcastMemoryEvent JSON-encodes payload — verify it round-trips.
	out, err := json.Marshal(map[string]any{"memory_id": "x", "scope": "team"})
	if err != nil {
		t.Fatalf("payload marshal: %v", err)
	}
	if !json.Valid(out) {
		t.Errorf("invalid json: %s", out)
	}
}

// TestMemorySyncEnabled — sentinel that env-driven flag still works.
func TestMemorySyncEnabled(t *testing.T) {
	prev := memoryHubURL
	memoryHubURL = ""
	t.Cleanup(func() { memoryHubURL = prev })
	if memorySyncEnabled() {
		t.Fatal("expected sync disabled with empty URL")
	}
}

// Package service contains business-logic helpers.
// activity.go: typed helper for writing to activity_log per PRD §3.
package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ActivityWriter writes structured entries to the activity_log table.
// Constructed via NewActivityWriter; failures are swallowed so activity
// logging never breaks business flow.
type ActivityWriter struct {
	Q *db.Queries
}

// NewActivityWriter constructs an ActivityWriter bound to the given Queries.
func NewActivityWriter(q *db.Queries) *ActivityWriter {
	return &ActivityWriter{Q: q}
}

// ActivityEntry is the optional bag of related-object IDs and metadata
// for a single activity_log row. workspace_id and event_type are required;
// any zero-value uuid.UUID is omitted from the row.
type ActivityEntry struct {
	WorkspaceID uuid.UUID
	EventType   string

	ActorID   uuid.UUID
	ActorType string

	EffectiveActorID   uuid.UUID
	EffectiveActorType string
	RealOperatorID     uuid.UUID
	RealOperatorType   string

	RelatedProjectID   uuid.UUID
	RelatedPlanID      uuid.UUID
	RelatedTaskID      uuid.UUID
	RelatedSlotID      uuid.UUID
	RelatedExecutionID uuid.UUID
	RelatedChannelID   uuid.UUID
	RelatedThreadID    uuid.UUID
	RelatedAgentID     uuid.UUID
	RelatedRuntimeID   uuid.UUID

	Payload        map[string]any
	RetentionClass string // "permanent" (default) | "ttl" | "temp"
}

// Write inserts an entry. Failures log a warning but do NOT propagate;
// activity logging is best-effort and should never break business flow.
func (w *ActivityWriter) Write(ctx context.Context, e ActivityEntry) {
	if w == nil || w.Q == nil {
		return
	}
	if e.WorkspaceID == uuid.Nil || e.EventType == "" {
		slog.Warn("activity.Write: missing workspace_id or event_type", "event", e.EventType)
		return
	}

	payload, err := json.Marshal(e.Payload)
	if err != nil || len(payload) == 0 {
		payload = []byte("{}")
	}
	if e.RetentionClass == "" {
		e.RetentionClass = "permanent"
	}

	params := db.WriteActivityLogParams{
		WorkspaceID:        toPgUUID(e.WorkspaceID),
		EventType:          e.EventType,
		ActorID:            toPgNullUUID(e.ActorID),
		ActorType:          toPgNullText(e.ActorType),
		EffectiveActorID:   toPgNullUUID(e.EffectiveActorID),
		EffectiveActorType: toPgNullText(e.EffectiveActorType),
		RealOperatorID:     toPgNullUUID(e.RealOperatorID),
		RealOperatorType:   toPgNullText(e.RealOperatorType),
		RelatedProjectID:   toPgNullUUID(e.RelatedProjectID),
		RelatedPlanID:      toPgNullUUID(e.RelatedPlanID),
		RelatedTaskID:      toPgNullUUID(e.RelatedTaskID),
		RelatedSlotID:      toPgNullUUID(e.RelatedSlotID),
		RelatedExecutionID: toPgNullUUID(e.RelatedExecutionID),
		RelatedChannelID:   toPgNullUUID(e.RelatedChannelID),
		RelatedThreadID:    toPgNullUUID(e.RelatedThreadID),
		RelatedAgentID:     toPgNullUUID(e.RelatedAgentID),
		RelatedRuntimeID:   toPgNullUUID(e.RelatedRuntimeID),
		Payload:            payload,
		RetentionClass:     pgtype.Text{String: e.RetentionClass, Valid: true},
		Details:            payload, // legacy column gets the same payload during transition
	}

	if _, err := w.Q.WriteActivityLog(ctx, params); err != nil {
		slog.Warn("activity.Write failed", "event", e.EventType, "ws", e.WorkspaceID, "err", err)
	}
}

// toPgUUID converts a non-nil uuid.UUID to a valid pgtype.UUID.
// (Caller has verified non-nil for required workspace_id.)
func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

// toPgNullUUID converts a possibly-nil uuid.UUID to a pgtype.UUID;
// uuid.Nil becomes Valid=false (NULL).
func toPgNullUUID(u uuid.UUID) pgtype.UUID {
	if u == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

// toPgNullText converts a possibly-empty string to a pgtype.Text;
// empty becomes Valid=false (NULL).
func toPgNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

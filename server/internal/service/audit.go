package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

type AuditService struct {
	Queries *db.Queries
}

func NewAuditService(q *db.Queries) *AuditService {
	return &AuditService{Queries: q}
}

// SubscribeToEvents registers a global event listener that auto-creates audit logs.
func (s *AuditService) SubscribeToEvents(bus *events.Bus) {
	bus.SubscribeAll(func(e events.Event) {
		go func() {
			details, _ := json.Marshal(e.Payload)

			ctx := context.Background()
			_, err := s.Queries.CreateActivity(ctx, db.CreateActivityParams{
				WorkspaceID: util.ParseUUID(e.WorkspaceID),
				IssueID:     pgtype.UUID{}, // no issue context for generic audit
				ActorType:   util.StrToText(e.ActorType),
				ActorID:     util.ParseUUID(e.ActorID),
				Action:      string(e.Type),
				Details:     details,
			})
			if err != nil {
				slog.Warn("audit log failed", "event", e.Type, "error", err)
			}
		}()
	})
}

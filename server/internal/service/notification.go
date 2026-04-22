package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type NotificationService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
}

func NewNotificationService(q *db.Queries, hub *realtime.Hub) *NotificationService {
	return &NotificationService{Queries: q, Hub: hub}
}

func (s *NotificationService) SubscribeToEvents(bus *events.Bus) {
	// Workflow step events → notify workspace
	bus.Subscribe("workflow:step:completed", func(e events.Event) {
		s.notifyWorkflowEvent(e, "Step completed")
	})
	bus.Subscribe("workflow:step:failed", func(e events.Event) {
		s.notifyWorkflowEvent(e, "Step failed — may need attention")
	})

	// Message events (already handled by auto-reply; placeholder for future inbox items)
	bus.Subscribe("message:created", func(e events.Event) {
	})

	// Agent status changes
	bus.Subscribe("impersonation:started", func(e events.Event) {
		slog.Info("impersonation started", "payload", e.Payload)
	})
}

func (s *NotificationService) notifyWorkflowEvent(e events.Event, title string) {
	go func() {
		ctx := context.Background()
		slog.Info("notification", "type", e.Type, "workspace", e.WorkspaceID, "title", title)

		if e.ActorID != "" && e.WorkspaceID != "" {
			_, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
				WorkspaceID:   util.ParseUUID(e.WorkspaceID),
				RecipientType: e.ActorType,
				RecipientID:   util.ParseUUID(e.ActorID),
				Type:          "workflow_update",
				Severity:      "info",
				Title:         title,
				Body:          util.StrToText(fmt.Sprintf("Event: %s", e.Type)),
				ActorType:     util.StrToText(e.ActorType),
				ActorID:       util.ParseUUID(e.ActorID),
			})
			if err != nil {
				slog.Warn("create notification failed", "error", err)
			}
		}
	}()
}

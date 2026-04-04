package service

import (
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	bus.Subscribe("agent:impersonation_started", func(e events.Event) {
		slog.Info("impersonation started", "payload", e.Payload)
	})
}

func (s *NotificationService) notifyWorkflowEvent(e events.Event, title string) {
	go func() {
		slog.Info("notification", "type", e.Type, "workspace", e.WorkspaceID, "title", title)
		// TODO: Create inbox item via CreateInboxItem query when recipient info is available
	}()
}

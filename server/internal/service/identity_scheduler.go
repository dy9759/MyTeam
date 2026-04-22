package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// IdentitySchedulerService periodically regenerates identity cards for all agents.
type IdentitySchedulerService struct {
	Queries   *db.Queries
	Generator *IdentityGeneratorService
}

// NewIdentitySchedulerService creates a new IdentitySchedulerService.
func NewIdentitySchedulerService(q *db.Queries, gen *IdentityGeneratorService) *IdentitySchedulerService {
	return &IdentitySchedulerService{Queries: q, Generator: gen}
}

// Start begins the 6-hour periodic identity card update loop.
func (s *IdentitySchedulerService) Start() {
	go func() {
		slog.Info("[identity-scheduler] Started (6h interval)")
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.updateAll(context.Background())
		}
	}()
}

func (s *IdentitySchedulerService) updateAll(ctx context.Context) {
	// List all non-archived agents across all workspaces
	agents, err := s.Queries.ListAllAgentsGlobal(ctx)
	if err != nil {
		slog.Warn("[identity-scheduler] failed to list agents", "error", err)
		return
	}

	updated := 0
	for _, agent := range agents {
		// Only update active agents (idle or working)
		if agent.Status != "idle" && agent.Status != "working" {
			continue
		}

		agentID := util.UUIDToString(agent.ID)
		workspaceID := util.UUIDToString(agent.WorkspaceID)

		if err := s.Generator.GenerateAndSave(ctx, agentID, workspaceID); err != nil {
			slog.Warn("[identity-scheduler] failed to generate card", "agent", agent.Name, "error", err)
			continue
		}
		updated++
		slog.Debug("[identity-scheduler] updated identity card", "agent", agent.Name)
	}

	slog.Info("[identity-scheduler] batch update complete", "updated", updated, "total", len(agents))
}

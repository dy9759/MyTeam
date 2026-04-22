package tools

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ListAssignedProjects returns projects the calling agent has at least one
// task assignment in (actual_agent_id or primary_assignee_id). Human callers
// are denied instead of falling back to the full workspace project list.
// Filters by status when supplied. Workspace-scoped.
type ListAssignedProjects struct{}

func (ListAssignedProjects) Name() string { return "list_assigned_projects" }

func (ListAssignedProjects) InputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]string{"type": "string"},
		},
	}
}

func (ListAssignedProjects) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ListAssignedProjects) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if ws.AgentID == uuid.Nil {
		return permissionDenied("agent context required to list assigned projects"), nil
	}

	wantStatus := stringArg(args, "status")

	projects, err := q.ListProjects(ctx, pgUUID(ws.WorkspaceID))
	if err != nil {
		return mcptool.Result{}, err
	}

	type projOut struct {
		ID          string `json:"id"`
		WorkspaceID string `json:"workspace_id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
	}
	out := []projOut{}
	for _, p := range projects {
		if wantStatus != "" && p.Status != wantStatus {
			continue
		}
		if ws.AgentID != uuid.Nil {
			plan, err := q.GetPlanByProject(ctx, p.ID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return mcptool.Result{}, err
			}
			tasks, err := q.ListTasksByPlan(ctx, plan.ID)
			if err != nil {
				return mcptool.Result{}, err
			}
			matched := false
			for _, t := range tasks {
				if (t.ActualAgentID.Valid && t.ActualAgentID.Bytes == ws.AgentID) ||
					(t.PrimaryAssigneeID.Valid && t.PrimaryAssigneeID.Bytes == ws.AgentID) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, projOut{
			ID:          uuidString(p.ID),
			WorkspaceID: uuidString(p.WorkspaceID),
			Title:       p.Title,
			Status:      p.Status,
		})
	}

	return mcptool.Result{Data: map[string]any{"projects": out}}, nil
}

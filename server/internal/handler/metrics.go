package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// MetricsResponse represents the aggregated workspace metrics.
// ---------------------------------------------------------------------------

type MetricsResponse struct {
	AgentResponseRate  *float64 `json:"agent_response_rate"`
	TaskCompletionRate *float64 `json:"task_completion_rate"`
	AverageTaskDuration *float64 `json:"average_task_duration_seconds"`
	TimeoutRate        *float64 `json:"timeout_rate"`
	ActiveProjects     int64    `json:"active_projects"`
	ActiveRuns         int64    `json:"active_runs"`
	PendingEscalations int64    `json:"pending_escalations"`
}

// ---------------------------------------------------------------------------
// GetWorkspaceMetrics — GET /api/metrics
//
// Returns aggregated metrics for the workspace:
// - agent_response_rate: responded / total needing response
// - task_completion_rate: completed / (completed + failed + cancelled)
// - average_task_duration: avg(completed_at - started_at) in seconds
// - timeout_rate: timed_out / total_dispatched
// - active_projects: count of running projects
// - active_runs: count of running project_runs
// - pending_escalations: count of inbox items with action_required and pending
// ---------------------------------------------------------------------------

func (h *Handler) GetWorkspaceMetrics(w http.ResponseWriter, r *http.Request) {
	_, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	ctx := r.Context()
	wsUUID := parseUUID(workspaceID)

	metrics := MetricsResponse{}

	// Task completion rate: completed / (completed + failed + cancelled)
	metrics.TaskCompletionRate = h.queryTaskCompletionRate(ctx, workspaceID)

	// Average task duration: avg(completed_at - started_at) for completed steps
	metrics.AverageTaskDuration = h.queryAverageTaskDuration(ctx, workspaceID)

	// Timeout rate: timed_out / total_dispatched
	metrics.TimeoutRate = h.queryTimeoutRate(ctx, workspaceID)

	// Pending escalations: inbox items with action_required = true and resolution_status = 'pending'
	metrics.PendingEscalations = h.queryPendingEscalations(ctx, wsUUID)

	// TODO: Active projects and runs require project/project_run tables.
	// Once those tables exist, uncomment:
	//   metrics.ActiveProjects = h.queryActiveProjects(ctx, workspaceID)
	//   metrics.ActiveRuns = h.queryActiveRuns(ctx, workspaceID)

	// Agent response rate requires message assignment tracking (future).
	// metrics.AgentResponseRate = h.queryAgentResponseRate(ctx, workspaceID)

	writeJSON(w, http.StatusOK, metrics)
}

// queryTaskCompletionRate computes completed / (completed + failed + cancelled)
// from the workflow_step table.
func (h *Handler) queryTaskCompletionRate(ctx context.Context, workspaceID string) *float64 {
	if h.DB == nil {
		return nil
	}

	query := `
		SELECT
			COUNT(*) FILTER (WHERE ws.status = 'completed') AS completed,
			COUNT(*) FILTER (WHERE ws.status IN ('completed', 'failed', 'cancelled')) AS total
		FROM workflow_step ws
		JOIN workflow w ON w.id = ws.workflow_id
		WHERE w.workspace_id = $1
		  AND ws.status IN ('completed', 'failed', 'cancelled')
	`

	var completed, total int64
	err := h.DB.QueryRow(ctx, query, workspaceID).Scan(&completed, &total)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Debug("metrics: failed to query task completion rate", "error", err)
		}
		return nil
	}

	if total == 0 {
		return nil
	}

	rate := float64(completed) / float64(total)
	return &rate
}

// queryAverageTaskDuration computes avg(completed_at - started_at) in seconds
// for completed workflow steps.
func (h *Handler) queryAverageTaskDuration(ctx context.Context, workspaceID string) *float64 {
	if h.DB == nil {
		return nil
	}

	query := `
		SELECT EXTRACT(EPOCH FROM AVG(ws.completed_at - ws.started_at))
		FROM workflow_step ws
		JOIN workflow w ON w.id = ws.workflow_id
		WHERE w.workspace_id = $1
		  AND ws.status = 'completed'
		  AND ws.started_at IS NOT NULL
		  AND ws.completed_at IS NOT NULL
	`

	var avg *float64
	err := h.DB.QueryRow(ctx, query, workspaceID).Scan(&avg)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Debug("metrics: failed to query average task duration", "error", err)
		}
		return nil
	}

	return avg
}

// queryTimeoutRate computes timed_out / total_dispatched from workflow steps.
// A step is considered "timed out" if it ever entered the 'timeout' status.
func (h *Handler) queryTimeoutRate(ctx context.Context, workspaceID string) *float64 {
	if h.DB == nil {
		return nil
	}

	query := `
		SELECT
			COUNT(*) FILTER (WHERE ws.status = 'timeout') AS timed_out,
			COUNT(*) AS total_dispatched
		FROM workflow_step ws
		JOIN workflow w ON w.id = ws.workflow_id
		WHERE w.workspace_id = $1
		  AND ws.status IN ('running', 'completed', 'failed', 'cancelled', 'timeout')
	`

	var timedOut, total int64
	err := h.DB.QueryRow(ctx, query, workspaceID).Scan(&timedOut, &total)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Debug("metrics: failed to query timeout rate", "error", err)
		}
		return nil
	}

	if total == 0 {
		return nil
	}

	rate := float64(timedOut) / float64(total)
	return &rate
}

// queryPendingEscalations counts inbox items with action_required = true
// and resolution_status = 'pending'.
func (h *Handler) queryPendingEscalations(ctx context.Context, workspaceID interface{}) int64 {
	if h.DB == nil {
		return 0
	}

	// Use a raw query since the inbox_item table may not yet have the
	// action_required and resolution_status columns. This will gracefully
	// return 0 if the columns don't exist.
	query := `
		SELECT COUNT(*)
		FROM inbox_item
		WHERE workspace_id = $1
		  AND action_required = true
		  AND resolution_status = 'pending'
	`

	var count int64
	err := h.DB.QueryRow(ctx, query, workspaceID).Scan(&count)
	if err != nil {
		// Columns may not exist yet; return 0.
		slog.Debug("metrics: failed to query pending escalations", "error", err)
		return 0
	}

	return count
}

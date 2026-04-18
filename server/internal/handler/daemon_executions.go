// Package handler: daemon_executions.go — Daemon HTTP endpoints for the
// Project Execution table per Plan 5 PRD §10.2. These run alongside the
// existing /api/daemon/runtimes/{id}/tasks/* endpoints (Issue link). The
// daemon is expected to poll BOTH; when both queues have work at the same
// priority, the Project Execution path wins.
//
// Auth: identical to the rest of /api/daemon — middleware.Auth(queries)
// resolves the daemon's PAT before reaching these handlers.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// GET /api/daemon/runtimes/{runtimeId}/executions/pending
// ---------------------------------------------------------------------------

// ListPendingExecutions returns up to 50 queued executions for the given
// runtime, ordered by (priority DESC, created_at ASC). The daemon normally
// jumps straight to ClaimExecution; this endpoint is for visibility and
// debugging.
func (h *Handler) ListPendingExecutions(w http.ResponseWriter, r *http.Request) {
	runtimeID, err := uuid.Parse(chi.URLParam(r, "runtimeId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime id")
		return
	}

	rows, err := h.Queries.ListPendingExecutionsForRuntime(r.Context(), pgUUIDFrom(runtimeID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		out = append(out, executionToResponse(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// POST /api/daemon/runtimes/{runtimeId}/executions/claim
// ---------------------------------------------------------------------------

// claimExecutionRequest is the daemon-supplied context recorded on the
// execution row when a claim succeeds. WorkingDir defaults to the runtime's
// configured working_dir when the daemon omits it.
type claimExecutionRequest struct {
	DaemonID   string `json:"daemon_id"`
	WorkingDir string `json:"working_dir"`
}

// ClaimExecution atomically transitions the next queued execution for a
// runtime into 'claimed' (FOR UPDATE SKIP LOCKED) and writes a context_ref
// describing the claim (mode/working_dir/daemon_id). Returns 204 when no
// work is available so the daemon can short-circuit its poll loop.
func (h *Handler) ClaimExecution(w http.ResponseWriter, r *http.Request) {
	runtimeID, err := uuid.Parse(chi.URLParam(r, "runtimeId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime id")
		return
	}

	var req claimExecutionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Default working_dir from the runtime row when the daemon doesn't
	// supply one (e.g. cloud executor poll). Look up the runtime so we can
	// also tag context_ref.mode = runtime.mode for downstream routing.
	mode := "local"
	if req.WorkingDir == "" || req.DaemonID == "" || mode == "" {
		rt, rtErr := h.Queries.GetAgentRuntime(r.Context(), pgUUIDFrom(runtimeID))
		if rtErr == nil {
			if req.WorkingDir == "" && rt.WorkingDir != "" {
				req.WorkingDir = rt.WorkingDir
			}
			if rt.Mode.Valid && rt.Mode.String != "" {
				mode = rt.Mode.String
			}
			if req.DaemonID == "" && rt.DaemonID.Valid {
				req.DaemonID = rt.DaemonID.String
			}
		}
	}

	contextRef, _ := json.Marshal(map[string]any{
		"mode":        mode,
		"working_dir": req.WorkingDir,
		"daemon_id":   req.DaemonID,
	})

	e, err := h.Queries.ClaimExecution(r.Context(), db.ClaimExecutionParams{
		RuntimeID:  pgUUIDFrom(runtimeID),
		ContextRef: contextRef,
	})
	if err != nil {
		// pgx.ErrNoRows = nothing to claim. The daemon retries later.
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "claim failed: "+err.Error())
		return
	}

	slog.Info("execution claimed",
		"execution_id", uuid.UUID(e.ID.Bytes).String(),
		"runtime_id", runtimeID,
		"task_id", uuid.UUID(e.TaskID.Bytes).String(),
		"daemon_id", req.DaemonID,
	)

	writeJSON(w, http.StatusOK, executionToResponse(e))
}

// ---------------------------------------------------------------------------
// POST /api/daemon/executions/{id}/start
// ---------------------------------------------------------------------------

// startExecutionRequest lets the daemon refresh context_ref when the
// execution actually starts running (e.g. attaching a session_id).
type startExecutionRequest struct {
	ContextRef map[string]any `json:"context_ref,omitempty"`
}

// StartExecution moves a claimed execution into running. Optional
// context_ref override lets the daemon attach the session id of the
// agent CLI process.
func (h *Handler) StartExecution(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req startExecutionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	var ctxRefJSON []byte
	if req.ContextRef != nil {
		ctxRefJSON, _ = json.Marshal(req.ContextRef)
	}

	if err := h.Queries.StartExecution(r.Context(), db.StartExecutionParams{
		ID:         pgUUIDFrom(id),
		ContextRef: ctxRefJSON,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "start failed: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// POST /api/daemon/executions/{id}/progress
// ---------------------------------------------------------------------------

// progressExecutionRequest carries an opaque progress blob (summary + step
// counters typically). The body is broadcast over WS as-is.
type progressExecutionRequest struct {
	Progress map[string]any `json:"progress"`
}

// ProgressExecution publishes a streaming progress event over the bus.
// Intentionally has no DB side effect — high-frequency progress writes
// would dirty the cost/lease columns for no real benefit.
func (h *Handler) ProgressExecution(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req progressExecutionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.publishExecutionEvent(r, "execution:progress", id, map[string]any{
		"execution_id": id.String(),
		"progress":     req.Progress,
	})

	w.WriteHeader(http.StatusAccepted)
}

// ---------------------------------------------------------------------------
// POST /api/daemon/executions/{id}/complete
// ---------------------------------------------------------------------------

// completeExecutionRequest mirrors the cost columns on the execution table.
// Pointers let the daemon omit individual fields without zeroing the cost
// record (the SQL uses COALESCE(narg, existing_value)).
type completeExecutionRequest struct {
	Result           map[string]any `json:"result"`
	CostInputTokens  *int           `json:"cost_input_tokens,omitempty"`
	CostOutputTokens *int           `json:"cost_output_tokens,omitempty"`
	CostUSD          *float64       `json:"cost_usd,omitempty"`
	CostProvider     string         `json:"cost_provider,omitempty"`
}

// CompleteExecution marks a running execution as completed, persists the
// result + cost, and hands off to SchedulerService so the Task state
// machine can advance (slots, downstream tasks, run completion check).
func (h *Handler) CompleteExecution(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req completeExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	resultJSON, _ := json.Marshal(req.Result)

	params := db.CompleteExecutionParams{
		ID:     pgUUIDFrom(id),
		Result: resultJSON,
	}
	if req.CostInputTokens != nil {
		params.CostInputTokens = pgtype.Int4{Int32: int32(*req.CostInputTokens), Valid: true}
	}
	if req.CostOutputTokens != nil {
		params.CostOutputTokens = pgtype.Int4{Int32: int32(*req.CostOutputTokens), Valid: true}
	}
	if req.CostUSD != nil {
		params.CostUsd = float64ToPgNumeric(*req.CostUSD)
	}
	if req.CostProvider != "" {
		params.CostProvider = pgtype.Text{String: req.CostProvider, Valid: true}
	}

	if err := h.Queries.CompleteExecution(r.Context(), params); err != nil {
		writeError(w, http.StatusInternalServerError, "complete failed: "+err.Error())
		return
	}

	// Hand off to the scheduler so the Task state machine advances. We
	// keep this best-effort: scheduler failures here are logged but do not
	// surface to the daemon — the execution row is already committed.
	if h.Scheduler != nil {
		e, getErr := h.Queries.GetExecution(r.Context(), pgUUIDFrom(id))
		if getErr != nil {
			slog.Warn("scheduler: GetExecution after complete failed",
				"execution_id", id, "err", getErr)
		} else {
			taskID := uuid.UUID(e.TaskID.Bytes)
			if err := h.Scheduler.HandleTaskCompletion(r.Context(), taskID, id, req.Result); err != nil {
				slog.Warn("scheduler: HandleTaskCompletion failed",
					"execution_id", id, "task_id", taskID, "err", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// POST /api/daemon/executions/{id}/fail
// ---------------------------------------------------------------------------

// failExecutionRequest distinguishes 'failed' (default) from 'timed_out'
// so the lifecycle ticker can route timeouts through the same endpoint.
type failExecutionRequest struct {
	Error  string `json:"error"`
	Status string `json:"status,omitempty"` // "failed" (default) or "timed_out"
}

// FailExecution marks an execution as failed (or timed_out) and hands off
// to SchedulerService.HandleTaskFailure to apply the retry / fallback
// policy on the parent Task.
func (h *Handler) FailExecution(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req failExecutionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Status == "" {
		req.Status = "failed"
	}
	// Defensively limit to the values allowed by the CHECK constraint
	// (migration 057). Anything else falls back to 'failed'.
	if req.Status != "failed" && req.Status != "timed_out" && req.Status != "cancelled" {
		req.Status = "failed"
	}

	if err := h.Queries.FailExecution(r.Context(), db.FailExecutionParams{
		ID:     pgUUIDFrom(id),
		Status: req.Status,
		Error:  pgtype.Text{String: req.Error, Valid: req.Error != ""},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "fail failed: "+err.Error())
		return
	}

	if h.Scheduler != nil {
		e, getErr := h.Queries.GetExecution(r.Context(), pgUUIDFrom(id))
		if getErr != nil {
			slog.Warn("scheduler: GetExecution after fail failed",
				"execution_id", id, "err", getErr)
		} else {
			taskID := uuid.UUID(e.TaskID.Bytes)
			if req.Status == "timed_out" {
				if err := h.Scheduler.HandleTaskTimeout(r.Context(), taskID); err != nil {
					slog.Warn("scheduler: HandleTaskTimeout failed",
						"execution_id", id, "task_id", taskID, "err", err)
				}
			} else if err := h.Scheduler.HandleTaskFailure(r.Context(), taskID, id, req.Error); err != nil {
				slog.Warn("scheduler: HandleTaskFailure failed",
					"execution_id", id, "task_id", taskID, "err", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// POST /api/daemon/executions/{id}/messages
// ---------------------------------------------------------------------------

// executionMessageRequest is an opaque message body — typically a tool
// call snapshot from the agent CLI's streaming output. Forwarded over WS
// without persistence.
type executionMessageRequest struct {
	Body map[string]any `json:"body"`
}

// StreamExecutionMessage publishes a single message append event over the
// bus so connected clients see live agent output. No DB write — message
// history for executions is intentionally ephemeral in this batch.
func (h *Handler) StreamExecutionMessage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req executionMessageRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.publishExecutionEvent(r, "execution:message", id, map[string]any{
		"execution_id": id.String(),
		"message":      req.Body,
	})

	w.WriteHeader(http.StatusAccepted)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// publishExecutionEvent fans out an execution-scoped WS event when the
// event bus is wired. Bus is optional so tests that bypass it remain
// usable. Workspace lookup is best-effort: a missing row falls back to a
// global broadcast (WorkspaceID = "") rather than failing the request.
func (h *Handler) publishExecutionEvent(r *http.Request, eventType string, executionID uuid.UUID, payload map[string]any) {
	if h.Bus == nil {
		return
	}
	workspaceID := ""
	ctx := r.Context()
	if exec, err := h.Queries.GetExecution(ctx, pgUUIDFrom(executionID)); err == nil {
		if task, err := h.Queries.GetTask(ctx, exec.TaskID); err == nil && task.WorkspaceID.Valid {
			workspaceID = uuid.UUID(task.WorkspaceID.Bytes).String()
		}
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// executionToResponse maps a db.Execution into the JSON shape the daemon
// (and clients) consume. Numeric / nullable columns are normalised so the
// daemon never has to inspect pgtype.* sentinel structs.
func executionToResponse(e db.Execution) map[string]any {
	out := map[string]any{
		"id":         uuid.UUID(e.ID.Bytes).String(),
		"task_id":    uuid.UUID(e.TaskID.Bytes).String(),
		"run_id":     uuid.UUID(e.RunID.Bytes).String(),
		"agent_id":   uuid.UUID(e.AgentID.Bytes).String(),
		"runtime_id": uuid.UUID(e.RuntimeID.Bytes).String(),
		"attempt":    e.Attempt,
		"status":     e.Status,
		"priority":   e.Priority,
		"payload":    rawJSONOrEmpty(e.Payload),
		"result":     rawJSONOrEmpty(e.Result),
		"context_ref": rawJSONOrEmpty(e.ContextRef),
		"cost_input_tokens":  e.CostInputTokens,
		"cost_output_tokens": e.CostOutputTokens,
		"log_retention_policy": e.LogRetentionPolicy,
	}
	if e.SlotID.Valid {
		out["slot_id"] = uuid.UUID(e.SlotID.Bytes).String()
	}
	if e.Error.Valid {
		out["error"] = e.Error.String
	}
	if e.CostProvider.Valid {
		out["cost_provider"] = e.CostProvider.String
	}
	if costStr, ok := numericString(e.CostUsd); ok {
		out["cost_usd"] = costStr
	}
	if e.ClaimedAt.Valid {
		out["claimed_at"] = e.ClaimedAt.Time
	}
	if e.StartedAt.Valid {
		out["started_at"] = e.StartedAt.Time
	}
	if e.CompletedAt.Valid {
		out["completed_at"] = e.CompletedAt.Time
	}
	if e.LogsExpiresAt.Valid {
		out["logs_expires_at"] = e.LogsExpiresAt.Time
	}
	if e.CreatedAt.Valid {
		out["created_at"] = e.CreatedAt.Time
	}
	if e.UpdatedAt.Valid {
		out["updated_at"] = e.UpdatedAt.Time
	}
	return out
}

// pgUUIDFrom converts a uuid.UUID into a pgtype.UUID. Mirrors the helper
// in service/activity.go but lives here so the handler package doesn't
// reach into the service package for it.
func pgUUIDFrom(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

// rawJSONOrEmpty returns the JSON body if non-empty, else json.RawMessage("{}")
// so clients always see valid JSON.
func rawJSONOrEmpty(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

// numericString returns the textual decimal representation of a numeric
// column. Used so the daemon receives a stable JSON-friendly cost value
// instead of pgx's internal struct.
func numericString(n pgtype.Numeric) (string, bool) {
	if !n.Valid {
		return "", false
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return "", false
	}
	return fmt.Sprintf("%.4f", f.Float64), true
}

// float64ToPgNumeric converts a JSON-decoded float64 into a pgtype.Numeric
// using a 4-decimal text round-trip. NUMERIC(10,4) on the column means
// extra precision is silently dropped server-side anyway.
func float64ToPgNumeric(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(fmt.Sprintf("%.4f", f)); err != nil {
		return pgtype.Numeric{}
	}
	return n
}

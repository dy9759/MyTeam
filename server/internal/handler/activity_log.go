package handler

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ActivityLogEntryResponse is the JSON shape for a single activity_log row.
// Optional FK fields are pointers so they serialize as null when invalid.
type ActivityLogEntryResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	EventType   string `json:"event_type"`
	Action      string `json:"action"`
	CreatedAt   string `json:"created_at"`

	ActorID   *string `json:"actor_id,omitempty"`
	ActorType *string `json:"actor_type,omitempty"`

	EffectiveActorID   *string `json:"effective_actor_id,omitempty"`
	EffectiveActorType *string `json:"effective_actor_type,omitempty"`
	RealOperatorID     *string `json:"real_operator_id,omitempty"`
	RealOperatorType   *string `json:"real_operator_type,omitempty"`

	RelatedProjectID   *string `json:"related_project_id,omitempty"`
	RelatedPlanID      *string `json:"related_plan_id,omitempty"`
	RelatedTaskID      *string `json:"related_task_id,omitempty"`
	RelatedSlotID      *string `json:"related_slot_id,omitempty"`
	RelatedExecutionID *string `json:"related_execution_id,omitempty"`
	RelatedChannelID   *string `json:"related_channel_id,omitempty"`
	RelatedThreadID    *string `json:"related_thread_id,omitempty"`
	RelatedAgentID     *string `json:"related_agent_id,omitempty"`
	RelatedRuntimeID   *string `json:"related_runtime_id,omitempty"`

	Payload         json.RawMessage `json:"payload"`
	RetentionClass  string          `json:"retention_class"`
	IssueID         *string         `json:"issue_id,omitempty"`
}

// ListActivityLog returns activity_log entries filtered by one of:
//   ?project_id=X     filter by related_project_id
//   ?task_id=X        filter by related_task_id
//   ?event_type=foo%  filter by event_type LIKE pattern
//
// Pagination via ?limit=N (default 50, max 200) and ?offset=N (default 0).
// Member-level access only: requires workspace membership. Per-row isolation
// (admin sees all, member only sees self-actor rows) is a follow-up — see
// PRD §3.4.
//
// Note: actor_id filter is intentionally not implemented for MVP — no
// matching sqlc query exists yet; will be added in a follow-up.
func (h *Handler) ListActivityLog(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	// Member-level access (admin/member isolation is TODO per PRD §3.4).
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	limit, offset := paginationFromRequest(r, 50, 200)
	wsUUID := parseUUID(workspaceID)

	if pid := r.URL.Query().Get("project_id"); pid != "" {
		rows, err := h.Queries.ListActivityLogByProject(r.Context(), db.ListActivityLogByProjectParams{
			WorkspaceID: wsUUID,
			ProjectID:   parseUUID(pid),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list activity log")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": activityRowsToResponse(rows)})
		return
	}

	if tid := r.URL.Query().Get("task_id"); tid != "" {
		rows, err := h.Queries.ListActivityLogByTask(r.Context(), db.ListActivityLogByTaskParams{
			WorkspaceID: wsUUID,
			TaskID:      parseUUID(tid),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list activity log")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": activityRowsToResponse(rows)})
		return
	}

	if etype := r.URL.Query().Get("event_type"); etype != "" {
		rows, err := h.Queries.ListActivityLogByEventType(r.Context(), db.ListActivityLogByEventTypeParams{
			WorkspaceID:      wsUUID,
			EventTypePattern: etype,
			LimitCount:       int32(limit),
			OffsetCount:      int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list activity log")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": activityRowsToResponse(rows)})
		return
	}

	writeError(w, http.StatusBadRequest, "must provide one of: project_id, task_id, event_type")
}

// activityRowsToResponse maps generated ActivityLog rows to the response shape.
func activityRowsToResponse(rows []db.ActivityLog) []ActivityLogEntryResponse {
	out := make([]ActivityLogEntryResponse, 0, len(rows))
	for _, r := range rows {
		payload := r.Payload
		if len(payload) == 0 {
			payload = []byte("{}")
		}
		out = append(out, ActivityLogEntryResponse{
			ID:                 uuidToString(r.ID),
			WorkspaceID:        uuidToString(r.WorkspaceID),
			EventType:          r.EventType,
			Action:             r.Action,
			CreatedAt:          timestampToString(r.CreatedAt),
			ActorID:            uuidToPtr(r.ActorID),
			ActorType:          textToPtr(r.ActorType),
			EffectiveActorID:   uuidToPtr(r.EffectiveActorID),
			EffectiveActorType: textToPtr(r.EffectiveActorType),
			RealOperatorID:     uuidToPtr(r.RealOperatorID),
			RealOperatorType:   textToPtr(r.RealOperatorType),
			RelatedProjectID:   uuidToPtr(r.RelatedProjectID),
			RelatedPlanID:      uuidToPtr(r.RelatedPlanID),
			RelatedTaskID:      uuidToPtr(r.RelatedTaskID),
			RelatedSlotID:      uuidToPtr(r.RelatedSlotID),
			RelatedExecutionID: uuidToPtr(r.RelatedExecutionID),
			RelatedChannelID:   uuidToPtr(r.RelatedChannelID),
			RelatedThreadID:    uuidToPtr(r.RelatedThreadID),
			RelatedAgentID:     uuidToPtr(r.RelatedAgentID),
			RelatedRuntimeID:   uuidToPtr(r.RelatedRuntimeID),
			Payload:            json.RawMessage(payload),
			RetentionClass:     r.RetentionClass,
			IssueID:            uuidToPtr(r.IssueID),
		})
	}
	return out
}

// paginationFromRequest is provided by inbox.go (same package).

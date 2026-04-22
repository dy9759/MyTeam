package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
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
//
// Per PRD §3.4 the result set is isolated by role:
//   - owners/admins see every row in the workspace, subject to the chosen filter
//   - members only see rows where they are the actor, are a participant on the
//     related project, or own the agent that ran the related task
//
// Note: actor_id filter is intentionally not implemented for MVP — no
// matching sqlc query exists yet; will be added in a follow-up.
func (h *Handler) ListActivityLog(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	pid := r.URL.Query().Get("project_id")
	tid := r.URL.Query().Get("task_id")
	etype := r.URL.Query().Get("event_type")
	if pid == "" && tid == "" && etype == "" {
		writeError(w, http.StatusBadRequest, "must provide one of: project_id, task_id, event_type")
		return
	}

	limit, offset := paginationFromRequest(r, 50, 200)
	wsUUID := parseUUID(workspaceID)

	// Owners and admins are not subject to per-row isolation.
	if roleAllowed(member.Role, "owner", "admin") {
		rows, err := h.listActivityForAdmin(r, wsUUID, pid, tid, etype, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list activity log")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": activityRowsToResponse(rows)})
		return
	}

	// Members get the isolated query — actor self / accessible project / owned agent task.
	//
	// Filter-handling divergence vs. the admin path above: the admin path
	// dispatches to a single per-filter sqlc query (first non-empty filter
	// wins), whereas the member query is one statement that ANDs every
	// supplied filter together. This is intentional — admins legitimately
	// hit each route variant in isolation, while members may pass multiple
	// filters at once (e.g. project_id + event_type) and we want all of
	// them to apply on top of the row-level visibility predicate. Keep
	// both behaviors as-is unless the API contract changes.
	params := db.ListActivityForMemberParams{
		WorkspaceID: wsUUID,
		SelfUserID:  member.UserID,
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	}
	if pid != "" {
		params.ProjectFilter = parseUUID(pid)
	}
	if tid != "" {
		params.TaskFilter = parseUUID(tid)
	}
	if etype != "" {
		params.EventTypeFilter = textOf(etype)
	}

	rows, err := h.Queries.ListActivityForMember(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activity log")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": activityRowsToResponse(rows)})
}

// listActivityForAdmin dispatches to the unfiltered (admin/owner) sqlc queries.
func (h *Handler) listActivityForAdmin(r *http.Request, wsUUID pgtype.UUID, pid, tid, etype string, limit, offset int) ([]db.ActivityLog, error) {
	switch {
	case pid != "":
		return h.Queries.ListActivityLogByProject(r.Context(), db.ListActivityLogByProjectParams{
			WorkspaceID: wsUUID,
			ProjectID:   parseUUID(pid),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
	case tid != "":
		return h.Queries.ListActivityLogByTask(r.Context(), db.ListActivityLogByTaskParams{
			WorkspaceID: wsUUID,
			TaskID:      parseUUID(tid),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
	default:
		return h.Queries.ListActivityLogByEventType(r.Context(), db.ListActivityLogByEventTypeParams{
			WorkspaceID:      wsUUID,
			EventTypePattern: etype,
			LimitCount:       int32(limit),
			OffsetCount:      int32(offset),
		})
	}
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

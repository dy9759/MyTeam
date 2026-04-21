// Package handler — agent interaction endpoints.
//
// Implements the agent-to-agent messaging layer added by migration 075.
// Design ported from AgentmeshHub's unified `interaction` protocol:
//
//   POST   /api/interactions            — send DM / broadcast / schema event
//   GET    /api/agents/:id/inbox        — poll pull fallback
//   POST   /api/interactions/:id/ack    — mark delivered/read
//
// The WS path (realtime.Hub.SendToAgent) is the push primary; this REST
// surface is the pull fallback. Both backed by the same `agent_interaction`
// table so an agent that missed a push still sees the message on poll.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// sendInteractionRequest mirrors the AgentMesh wire format
// 1:1 so the client-side protocol can stay a single struct across
// projects. `target` is union-typed — exactly one field is expected
// per call and the rest are ignored.
type sendInteractionRequest struct {
	Type        string            `json:"type"`         // message / task / query / event / broadcast
	ContentType string            `json:"content_type"` // text / json / file (default text)
	Target      interactionTarget `json:"target"`
	Schema      string            `json:"schema,omitempty"`
	Payload     json.RawMessage   `json:"payload"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
}

type interactionTarget struct {
	AgentID    string `json:"agent_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Capability string `json:"capability,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
}

type interactionResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id,omitempty"`
	FromID      string          `json:"from_id"`
	FromType    string          `json:"from_type"`
	Target      map[string]any  `json:"target"`
	Type        string          `json:"type"`
	ContentType string          `json:"content_type"`
	Schema      string          `json:"schema,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   string          `json:"created_at"`
}

// SendInteraction — POST /api/interactions
func (h *Handler) SendInteraction(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req sendInteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !isValidInteractionType(req.Type) {
		writeError(w, http.StatusBadRequest, "invalid type; expected one of message/task/query/event/broadcast")
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}

	// Exactly-one-of target rule enforced at the handler so callers
	// get a readable 400 rather than NULL-constraint errors.
	targetCount := 0
	if req.Target.AgentID != "" {
		targetCount++
	}
	if req.Target.Channel != "" {
		targetCount++
	}
	if req.Target.Capability != "" {
		targetCount++
	}
	if req.Target.SessionID != "" {
		targetCount++
	}
	if targetCount != 1 {
		writeError(w, http.StatusBadRequest, "target must specify exactly one of agent_id / channel / capability / session_id")
		return
	}

	// Resolve sender identity: if the caller asserts an agent identity
	// via X-Agent-ID, we validated that already in resolveActor.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	metadata := req.Metadata
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	params := db.CreateAgentInteractionParams{
		WorkspaceID: parseUUID(workspaceID),
		FromID:      parseUUID(actorID),
		FromType:    actorType,
		ToAgentID:   parseUUID(req.Target.AgentID),
		Channel:     textOrNull(req.Target.Channel),
		Capability:  textOrNull(req.Target.Capability),
		SessionID:   parseUUID(req.Target.SessionID),
		Type:        req.Type,
		ContentType: req.ContentType,
		Schema:      textOrNull(req.Schema),
		Payload:     payload,
		Metadata:    metadata,
	}

	row, err := h.Queries.CreateAgentInteraction(r.Context(), params)
	if err != nil {
		slog.Error("create agent interaction", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create interaction")
		return
	}

	resp := interactionRow(row)

	// Push to the addressed agent via WS. DM and capability-broadcast
	// both need a fan-out; for now the handler only pushes DM — the
	// capability fan-out ticket requires an agent-by-capability index
	// (follow-up once subagent.category is mapped onto it).
	if h.Hub != nil && req.Target.AgentID != "" {
		h.Hub.PushToAgent(req.Target.AgentID, map[string]any{
			"type":    "interaction",
			"payload": resp,
		})
	}

	writeJSON(w, http.StatusCreated, resp)
}

// GetAgentInbox — GET /api/agents/:id/inbox?after=<rfc3339>&limit=N
func (h *Handler) GetAgentInbox(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent id required")
		return
	}

	// Agent must belong to the resolved workspace.
	agent, err := h.Queries.GetAgent(r.Context(), parseUUID(agentID))
	if err != nil || uuidToString(agent.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}

	limit := int32(50)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}
	var after pgtype.Timestamptz
	if v := r.URL.Query().Get("after"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			after = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	rows, err := h.Queries.ListAgentInbox(r.Context(), db.ListAgentInboxParams{
		ToAgentID: parseUUID(agentID),
		After:     after,
		Limit:     limit,
	})
	if err != nil {
		slog.Error("list agent inbox", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	out := make([]interactionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, interactionRow(row))
		// Mark as delivered on read — pull-fallback ack. Explicit
		// /ack endpoint still supports read-later semantics.
		_ = h.Queries.MarkAgentInteractionDelivered(r.Context(), row.ID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"interactions": out,
		"count":        len(out),
	})
}

// AckInteraction — POST /api/interactions/:id/ack?state=read|delivered
func (h *Handler) AckInteraction(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "interaction id required")
		return
	}
	uid := parseUUID(id)

	state := r.URL.Query().Get("state")
	if state == "" {
		state = "delivered"
	}

	switch state {
	case "delivered":
		if err := h.Queries.MarkAgentInteractionDelivered(r.Context(), uid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to ack")
			return
		}
	case "read":
		if err := h.Queries.MarkAgentInteractionRead(r.Context(), uid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to ack")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "state must be 'delivered' or 'read'")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func isValidInteractionType(t string) bool {
	switch t {
	case "message", "task", "query", "event", "broadcast":
		return true
	}
	return false
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func interactionRow(row db.AgentInteraction) interactionResponse {
	target := map[string]any{}
	if row.ToAgentID.Valid {
		target["agent_id"] = uuidToString(row.ToAgentID)
	}
	if row.Channel.Valid {
		target["channel"] = row.Channel.String
	}
	if row.Capability.Valid {
		target["capability"] = row.Capability.String
	}
	if row.SessionID.Valid {
		target["session_id"] = uuidToString(row.SessionID)
	}

	schema := ""
	if row.Schema.Valid {
		schema = row.Schema.String
	}
	createdAt := ""
	if row.CreatedAt.Valid {
		createdAt = row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}

	return interactionResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		FromID:      uuidToString(row.FromID),
		FromType:    row.FromType,
		Target:      target,
		Type:        row.Type,
		ContentType: row.ContentType,
		Schema:      schema,
		Payload:     json.RawMessage(row.Payload),
		Metadata:    json.RawMessage(row.Metadata),
		Status:      row.Status,
		CreatedAt:   createdAt,
	}
}

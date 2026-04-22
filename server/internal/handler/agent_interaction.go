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
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// CanActAsAgent returns true when userID is allowed to send or read
// messages on behalf of agentID inside the resolved workspace. Three
// paths grant permission, tried in order of specificity:
//
//  1. Personal ownership — `agent.owner_id = userID`.
//  2. Active impersonation session — owner_id=userID, agent_id=agentID,
//     not expired, not ended.
//  3. Workspace ops override — userID has role owner or admin in the
//     same workspace the agent belongs to.
//
// All three branches silently return false on DB error; the caller
// treats any negative as a 403, so we never leak the reason.
func (h *Handler) CanActAsAgent(ctx context.Context, userID, agentID, workspaceID string) bool {
	if userID == "" || agentID == "" || workspaceID == "" {
		return false
	}

	agent, err := h.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		return false
	}
	if uuidToString(agent.WorkspaceID) != workspaceID {
		return false
	}

	// 1. Personal ownership.
	if uuidToString(agent.OwnerID) == userID {
		return true
	}

	// 2. Active impersonation session.
	imp, err := h.Queries.GetActiveImpersonation(ctx, parseUUID(agentID))
	if err == nil && uuidToString(imp.OwnerID) == userID {
		return true
	}

	// 3. Workspace owner / admin override.
	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err == nil && (member.Role == "owner" || member.Role == "admin") {
		return true
	}

	return false
}

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

// interactionResponse is the wire shape returned to clients. It
// intentionally omits workspace_id — that field is server-side scoping
// only and the receiver already knows what workspace they're in via
// their own session. Surfacing it leaked the sender's workspace once
// cross-workspace validation landed.
type interactionResponse struct {
	ID          string          `json:"id"`
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
	// content_type='json' claims the payload is a JSON document — reject
	// garbage early rather than letting receivers hit a parse error.
	if req.ContentType == "json" && len(req.Payload) > 0 {
		var probe any
		if err := json.Unmarshal(req.Payload, &probe); err != nil {
			writeError(w, http.StatusBadRequest, "payload is not valid JSON")
			return
		}
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

	// Resolve sender identity. X-Agent-ID means "I'm speaking as this
	// agent" — resolveActor already checked the agent lives in this
	// workspace, but that isn't enough: anyone in the workspace could
	// otherwise claim to be any agent. Re-check via canActAsAgent.
	// resolveActor returns 'member' for humans; the DB check constraint
	// uses 'user', so translate.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		if !h.CanActAsAgent(r.Context(), userID, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "not allowed to send as this agent")
			return
		}
	} else {
		actorType = "user"
	}

	// Target validation — every reachable target must live in the
	// sender's workspace. Without this, a cross-workspace agent id or
	// session id leaks through the FK since agent_interaction.to_agent_id
	// is not scoped to workspace_id at the schema level.
	if req.Target.AgentID != "" {
		targetAgent, err := h.Queries.GetAgent(r.Context(), parseUUID(req.Target.AgentID))
		if err != nil || uuidToString(targetAgent.WorkspaceID) != workspaceID {
			writeError(w, http.StatusNotFound, "target agent not found in this workspace")
			return
		}
	}

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
	delivered := 0

	// DM push via WS. Hub.PushToAgent dedups via Client.recentPushes, so
	// a reconnecting agent won't see duplicates. When delivery succeeds
	// we flip the row to `delivered` synchronously — otherwise a WS
	// receiver that processed the message would still see it again on
	// next REST poll (status stuck at pending).
	if h.Hub != nil && req.Target.AgentID != "" {
		delivered = h.Hub.PushToAgent(req.Target.AgentID, map[string]any{
			"type":    "interaction",
			"payload": resp,
		})
	}

	// Capability fan-out. Match every non-archived agent in the
	// workspace whose `category` equals the requested capability, then
	// push. Each delivery gets its own row so receivers ack independently.
	if h.Hub != nil && req.Target.Capability != "" {
		delivered += h.fanOutCapability(r.Context(), workspaceID, req.Target.Capability, resp)
	}

	if delivered > 0 {
		if err := h.Queries.MarkAgentInteractionDelivered(r.Context(), row.ID); err == nil {
			resp.Status = "delivered"
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// fanOutCapability pushes `base` to every workspace agent whose
// `category` matches the capability string. Each WS push is counted;
// returns the total delivered clients (multiple per agent when a user
// has several tabs open). Capability broadcast does not create one
// row per receiver — the single row carries the capability label, and
// `ListAgentInteractionsByCapability` is the pull fallback.
func (h *Handler) fanOutCapability(ctx context.Context, workspaceID, capability string, base interactionResponse) int {
	agents, err := h.Queries.ListAllAgents(ctx, parseUUID(workspaceID))
	if err != nil {
		return 0
	}
	delivered := 0
	for _, a := range agents {
		if a.ArchivedAt.Valid {
			continue
		}
		// Subagent rows are templates, not runnable receivers — skip
		// them even when their category matches the capability.
		if a.Kind != "agent" {
			continue
		}
		if !strings.EqualFold(a.Category, capability) {
			continue
		}
		delivered += h.Hub.PushToAgent(uuidToString(a.ID), map[string]any{
			"type":    "interaction",
			"payload": base,
		})
	}
	return delivered
}

// GetAgentInbox — GET /api/agents/:id/inbox?after=<rfc3339>&limit=N
func (h *Handler) GetAgentInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
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

	// Reading an agent's inbox is an act-as-agent permission, not a
	// plain workspace-member one. Otherwise a member could harvest
	// messages addressed to any agent in their workspace.
	if !h.CanActAsAgent(r.Context(), userID, agentID, workspaceID) {
		writeError(w, http.StatusForbidden, "not allowed to read this agent's inbox")
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
			after = timestamptzOf(t)
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
	pending := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, interactionRow(row))
		if row.Status == "pending" {
			pending = append(pending, row.ID)
		}
	}
	// Bulk mark-delivered — one UPDATE per page instead of N. Best-
	// effort: failure just leaves rows at 'pending' so the next poll
	// retries naturally.
	if len(pending) > 0 {
		if err := h.Queries.MarkAgentInteractionsDelivered(r.Context(), pending); err != nil {
			slog.Warn("bulk mark delivered", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"interactions": out,
		"count":        len(out),
	})
}

// AckInteraction — POST /api/interactions/:id/ack?state=read|delivered
func (h *Handler) AckInteraction(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "interaction id required")
		return
	}
	uid := parseUUID(id)

	// Ack is an act-as-agent operation on the addressee. Load the row
	// first so we can confirm the caller actually owns / impersonates
	// the recipient — otherwise anyone could flip someone else's inbox
	// to `read` and hide messages.
	existing, err := h.Queries.GetAgentInteraction(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	if !existing.ToAgentID.Valid {
		writeError(w, http.StatusBadRequest, "cannot ack a non-DM interaction")
		return
	}
	if !h.CanActAsAgent(r.Context(), userID, uuidToString(existing.ToAgentID), workspaceID) {
		writeError(w, http.StatusForbidden, "not allowed to ack this interaction")
		return
	}

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

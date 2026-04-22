package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func (h *Handler) CreateRemoteSession(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		AgentID     string `json:"agent_id"`
		Title       string `json:"title"`
		Environment any    `json:"environment"`
	}
	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	envJSON, _ := json.Marshal(req.Environment)

	rs, err := h.Queries.CreateRemoteSession(r.Context(), db.CreateRemoteSessionParams{
		AgentID:     parseUUID(req.AgentID),
		WorkspaceID: parseUUID(workspaceID),
		OwnerID:     parseUUID(userID),
		Title:       ptrToText(&req.Title),
		Environment: envJSON,
	})
	if err != nil {
		slog.Warn("create remote session failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create remote session")
		return
	}
	writeJSON(w, http.StatusCreated, remoteSessionToResponse(rs))
}

func (h *Handler) GetRemoteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "remoteSessionID")
	rs, err := h.Queries.GetRemoteSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "remote session not found")
		return
	}
	writeJSON(w, http.StatusOK, remoteSessionToResponse(rs))
}

func (h *Handler) ListRemoteSessions(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)

	sessions, err := h.Queries.ListRemoteSessionsByWorkspace(r.Context(), db.ListRemoteSessionsByWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list remote sessions")
		return
	}
	resp := make([]map[string]any, len(sessions))
	for i, rs := range sessions {
		resp[i] = remoteSessionToResponse(rs)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": resp})
}

func (h *Handler) UpdateRemoteSessionStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "remoteSessionID")
	type Req struct {
		Status string `json:"status"`
	}
	var req Req
	json.NewDecoder(r.Body).Decode(&req)

	h.Queries.UpdateRemoteSessionStatus(r.Context(), db.UpdateRemoteSessionStatusParams{
		ID: parseUUID(id), Status: req.Status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

func (h *Handler) AddRemoteSessionEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "remoteSessionID")
	type Req struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}
	var req Req
	json.NewDecoder(r.Body).Decode(&req)

	eventJSON, _ := json.Marshal([]map[string]any{{"type": req.Type, "data": req.Data, "timestamp": time.Now().Format(time.RFC3339)}})
	h.Queries.AddRemoteSessionEvent(r.Context(), db.AddRemoteSessionEventParams{
		ID: parseUUID(id), Column2: eventJSON,
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "event_added": req.Type})
}

func remoteSessionToResponse(rs db.RemoteSession) map[string]any {
	return map[string]any{
		"id":           uuidToString(rs.ID),
		"agent_id":     uuidToString(rs.AgentID),
		"workspace_id": uuidToString(rs.WorkspaceID),
		"owner_id":     uuidToString(rs.OwnerID),
		"status":       rs.Status,
		"title":        textToPtr(rs.Title),
		"events":       json.RawMessage(rs.Events),
		"created_at":   timestampToString(rs.CreatedAt),
		"updated_at":   timestampToString(rs.UpdatedAt),
	}
}

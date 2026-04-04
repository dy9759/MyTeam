package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------- response types ----------

type SessionResponse struct {
	ID          string                       `json:"id"`
	WorkspaceID string                       `json:"workspace_id"`
	Title       string                       `json:"title"`
	CreatorID   string                       `json:"creator_id"`
	CreatorType string                       `json:"creator_type"`
	Status      string                       `json:"status"`
	MaxTurns    int32                        `json:"max_turns"`
	CurrentTurn int32                        `json:"current_turn"`
	Context     json.RawMessage              `json:"context,omitempty"`
	IssueID     *string                      `json:"issue_id,omitempty"`
	CreatedAt   string                       `json:"created_at"`
	UpdatedAt   string                       `json:"updated_at"`
	Participants []SessionParticipantResponse `json:"participants,omitempty"`
}

type SessionParticipantResponse struct {
	ParticipantID   string `json:"participant_id"`
	ParticipantType string `json:"participant_type"`
	Role            string `json:"role"`
	JoinedAt        string `json:"joined_at"`
}

func sessionToResponse(s db.Session) SessionResponse {
	return SessionResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Title:       s.Title,
		CreatorID:   uuidToString(s.CreatorID),
		CreatorType: s.CreatorType,
		Status:      s.Status,
		MaxTurns:    s.MaxTurns,
		CurrentTurn: s.CurrentTurn,
		Context:     s.Context,
		IssueID:     uuidToPtr(s.IssueID),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func participantToResponse(p db.SessionParticipant) SessionParticipantResponse {
	return SessionParticipantResponse{
		ParticipantID:   uuidToString(p.ParticipantID),
		ParticipantType: p.ParticipantType,
		Role:            p.Role,
		JoinedAt:        timestampToString(p.JoinedAt),
	}
}

// ---------- handlers ----------

// POST /api/sessions
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	type participantInput struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Role string `json:"role"`
	}
	type createReq struct {
		Title        string            `json:"title"`
		IssueID      *string           `json:"issue_id,omitempty"`
		MaxTurns     int32             `json:"max_turns,omitempty"`
		Context      json.RawMessage   `json:"context,omitempty"`
		Participants []participantInput `json:"participants"`
	}

	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.MaxTurns <= 0 {
		req.MaxTurns = 50
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	var issueUUID pgtype.UUID
	if req.IssueID != nil {
		issueUUID = parseUUID(*req.IssueID)
	}

	session, err := h.Queries.CreateSession(ctx, db.CreateSessionParams{
		WorkspaceID: parseUUID(workspaceID),
		Title:       req.Title,
		CreatorID:   parseUUID(actorID),
		CreatorType: actorType,
		MaxTurns:    req.MaxTurns,
		Context:     req.Context,
		IssueID:     issueUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Add creator as participant with role "creator".
	_ = h.Queries.AddSessionParticipant(ctx, db.AddSessionParticipantParams{
		SessionID:       session.ID,
		ParticipantID:   parseUUID(actorID),
		ParticipantType: actorType,
		Role:            "creator",
	})

	// Add additional participants.
	for _, p := range req.Participants {
		role := p.Role
		if role == "" {
			role = "participant"
		}
		_ = h.Queries.AddSessionParticipant(ctx, db.AddSessionParticipantParams{
			SessionID:       session.ID,
			ParticipantID:   parseUUID(p.ID),
			ParticipantType: p.Type,
			Role:            role,
		})
	}

	h.publish("session:created", workspaceID, actorType, actorID, sessionToResponse(session))
	writeJSON(w, http.StatusCreated, sessionToResponse(session))
}

// GET /api/sessions
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	limit := int32(100)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = int32(v)
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = int32(v)
		}
	}

	sessions, err := h.Queries.ListSessions(ctx, db.ListSessionsParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	out := make([]SessionResponse, len(sessions))
	for i, s := range sessions {
		out[i] = sessionToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// GET /api/sessions/{sessionID}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	session, err := h.Queries.GetSession(ctx, parseUUID(sessionID))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	participants, err := h.Queries.ListSessionParticipants(ctx, session.ID)
	if err != nil {
		participants = nil
	}

	resp := sessionToResponse(session)
	resp.Participants = make([]SessionParticipantResponse, len(participants))
	for i, p := range participants {
		resp.Participants[i] = participantToResponse(p)
	}
	writeJSON(w, http.StatusOK, resp)
}

// PATCH /api/sessions/{sessionID}
func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	sessionID := chi.URLParam(r, "sessionID")

	type updateReq struct {
		Status  *string         `json:"status,omitempty"`
		Context json.RawMessage `json:"context,omitempty"`
	}
	var req updateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sid := parseUUID(sessionID)

	if req.Status != nil {
		if err := h.Queries.UpdateSessionStatus(ctx, db.UpdateSessionStatusParams{
			ID:     sid,
			Status: *req.Status,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update session status")
			return
		}
	}

	if req.Context != nil {
		if err := h.Queries.UpdateSessionContext(ctx, db.UpdateSessionContextParams{
			ID:      sid,
			Context: req.Context,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update session context")
			return
		}
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("session:updated", workspaceID, actorType, actorID, map[string]string{"session_id": sessionID})

	// Return updated session.
	session, err := h.Queries.GetSession(ctx, sid)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sessionToResponse(session))
}

// POST /api/sessions/{sessionID}/join
func (h *Handler) JoinSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	sessionID := chi.URLParam(r, "sessionID")
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	err := h.Queries.AddSessionParticipant(ctx, db.AddSessionParticipantParams{
		SessionID:       parseUUID(sessionID),
		ParticipantID:   parseUUID(actorID),
		ParticipantType: actorType,
		Role:            "participant",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join session")
		return
	}

	h.publish("session:participant_joined", workspaceID, actorType, actorID, map[string]string{
		"session_id":     sessionID,
		"participant_id": actorID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/sessions/{sessionID}/messages
func (h *Handler) ListSessionMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")

	limit := int32(100)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = int32(v)
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = int32(v)
		}
	}

	messages, err := h.Queries.ListSessionMessages(ctx, db.ListSessionMessagesParams{
		SessionID: parseUUID(sessionID),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

// GET /api/sessions/{sessionID}/summary
func (h *Handler) SessionSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")

	messages, err := h.Queries.ListSessionMessages(ctx, db.ListSessionMessagesParams{
		SessionID: parseUUID(sessionID),
		Limit:     1000,
		Offset:    0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	participants, _ := h.Queries.ListSessionParticipants(ctx, parseUUID(sessionID))

	// Count by sender.
	senderCounts := map[string]int{}
	type timelineEntry struct {
		SenderID   string `json:"sender_id"`
		SenderType string `json:"sender_type"`
		CreatedAt  string `json:"created_at"`
	}
	var timeline []timelineEntry
	for _, m := range messages {
		key := uuidToString(m.SenderID)
		senderCounts[key]++
		timeline = append(timeline, timelineEntry{
			SenderID:   key,
			SenderType: m.SenderType,
			CreatedAt:  timestampToString(m.CreatedAt),
		})
	}

	partResp := make([]SessionParticipantResponse, len(participants))
	for i, p := range participants {
		partResp[i] = participantToResponse(p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message_count": len(messages),
		"sender_counts": senderCounts,
		"participants":  partResp,
		"timeline":      timeline,
	})
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type WorkspaceCollaboratorResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	AddedBy     *string `json:"added_by"`
	AddedAt     string  `json:"added_at"`
}

type BrowserContextResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Domain      *string  `json:"domain"`
	Status      string   `json:"status"`
	CreatedBy   string   `json:"created_by"`
	SharedWith  []string `json:"shared_with"`
	CreatedAt   string   `json:"created_at"`
	LastUsedAt  string   `json:"last_used_at"`
}

type BrowserTabResponse struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	URL            string   `json:"url"`
	Title          *string  `json:"title"`
	Status         string   `json:"status"`
	CreatedBy      string   `json:"created_by"`
	SharedWith     []string `json:"shared_with"`
	ContextID      *string  `json:"context_id"`
	SessionID      *string  `json:"session_id"`
	LiveURL        *string  `json:"live_url"`
	ScreenshotURL  *string  `json:"screenshot_url"`
	ConversationID *string  `json:"conversation_id"`
	ProjectID      *string  `json:"project_id"`
	CreatedAt      string   `json:"created_at"`
	LastActiveAt   string   `json:"last_active_at"`
}

type WorkspaceSnapshotResponse struct {
	Workspace       WorkspaceResponse               `json:"workspace"`
	Agents          []AgentResponse                 `json:"agents"`
	Conversations   []map[string]any                `json:"conversations"`
	Channels        []map[string]any                `json:"channels"`
	Files           []FileIndexResponse             `json:"files"`
	BrowserTabs     []BrowserTabResponse            `json:"browser_tabs"`
	BrowserContexts []BrowserContextResponse        `json:"browser_contexts"`
	Collaborators   []WorkspaceCollaboratorResponse `json:"collaborators"`
	Inbox           map[string]any                  `json:"inbox"`
	Runtimes        []AgentRuntimeResponse          `json:"runtimes"`
}

func collaboratorToResponse(collaborator db.WorkspaceCollaborator) WorkspaceCollaboratorResponse {
	return WorkspaceCollaboratorResponse{
		ID:          uuidToString(collaborator.ID),
		WorkspaceID: uuidToString(collaborator.WorkspaceID),
		Email:       collaborator.Email,
		Role:        collaborator.Role,
		AddedBy:     textToPtr(collaborator.AddedBy),
		AddedAt:     timestampToString(collaborator.AddedAt),
	}
}

func decodeStringArray(data []byte) []string {
	if len(data) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return []string{}
	}
	return values
}

func browserContextToResponse(context db.BrowserContext) BrowserContextResponse {
	return BrowserContextResponse{
		ID:          uuidToString(context.ID),
		WorkspaceID: uuidToString(context.WorkspaceID),
		Name:        context.Name,
		Domain:      textToPtr(context.Domain),
		Status:      context.Status,
		CreatedBy:   context.CreatedBy,
		SharedWith:  decodeStringArray(context.SharedWith),
		CreatedAt:   timestampToString(context.CreatedAt),
		LastUsedAt:  timestampToString(context.LastUsedAt),
	}
}

func browserTabToResponse(tab db.BrowserTab) BrowserTabResponse {
	return BrowserTabResponse{
		ID:             uuidToString(tab.ID),
		WorkspaceID:    uuidToString(tab.WorkspaceID),
		URL:            tab.Url,
		Title:          textToPtr(tab.Title),
		Status:         tab.Status,
		CreatedBy:      tab.CreatedBy,
		SharedWith:     decodeStringArray(tab.SharedWith),
		ContextID:      uuidToPtr(tab.ContextID),
		SessionID:      textToPtr(tab.SessionID),
		LiveURL:        textToPtr(tab.LiveUrl),
		ScreenshotURL:  textToPtr(tab.ScreenshotUrl),
		ConversationID: uuidToPtr(tab.ConversationID),
		ProjectID:      uuidToPtr(tab.ProjectID),
		CreatedAt:      timestampToString(tab.CreatedAt),
		LastActiveAt:   timestampToString(tab.LastActiveAt),
	}
}

func normalizeCollaboratorEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", fmt.Errorf("email is required")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("invalid email")
	}
	return strings.ToLower(parsed.Address), nil
}

func (h *Handler) ListWorkspaceCollaborators(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	items, err := h.Queries.ListWorkspaceCollaborators(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list collaborators")
		return
	}

	resp := make([]WorkspaceCollaboratorResponse, len(items))
	for i, item := range items {
		resp[i] = collaboratorToResponse(item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"collaborators": resp})
}

func (h *Handler) CreateWorkspaceCollaborator(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email, err := normalizeCollaboratorEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "editor"
	}
	if role != "editor" && role != "viewer" {
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}

	item, err := h.Queries.CreateWorkspaceCollaborator(r.Context(), db.CreateWorkspaceCollaboratorParams{
		WorkspaceID: parseUUID(workspaceID),
		Email:       email,
		Role:        role,
		AddedBy:     strToText("member:" + uuidToString(member.UserID)),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "collaborator already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create collaborator")
		return
	}

	writeJSON(w, http.StatusCreated, collaboratorToResponse(item))
}

func (h *Handler) DeleteWorkspaceCollaborator(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	emailValue := chi.URLParam(r, "email")
	email, err := normalizeCollaboratorEmail(emailValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.Queries.DeleteWorkspaceCollaborator(r.Context(), db.DeleteWorkspaceCollaboratorParams{
		WorkspaceID: parseUUID(workspaceID),
		Email:       email,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete collaborator")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listDMConversations(ctx context.Context, workspaceID, userID string) ([]map[string]any, error) {
	if h.DB == nil {
		return []map[string]any{}, nil
	}

	rows, err := h.DB.Query(ctx, `
		WITH dm_messages AS (
			SELECT
				CASE
					WHEN sender_id = $2::uuid AND sender_type = 'member' THEN recipient_id
					ELSE sender_id
				END AS peer_id,
				CASE
					WHEN sender_id = $2::uuid AND sender_type = 'member' THEN COALESCE(recipient_type, 'member')
					ELSE sender_type
				END AS peer_type,
				id,
				content,
				status,
				created_at,
				sender_id
			FROM message
			WHERE workspace_id = $1::uuid
				AND channel_id IS NULL
				AND (
					(sender_id = $2::uuid AND sender_type = 'member')
					OR (recipient_id = $2::uuid AND recipient_type = 'member')
				)
		),
		latest AS (
			SELECT DISTINCT ON (dm.peer_id, dm.peer_type)
				dm.peer_id,
				dm.peer_type,
				COALESCE(u.name, a.name, dm.peer_id::text) AS peer_name,
				dm.id,
				dm.content,
				dm.created_at,
				(
					SELECT COUNT(*)
					FROM dm_messages unread
					WHERE unread.peer_id = dm.peer_id
						AND unread.peer_type = dm.peer_type
						AND unread.sender_id <> $2::uuid
						AND unread.status = 'sent'
				) AS unread_count
			FROM dm_messages dm
			LEFT JOIN "user" u ON dm.peer_type = 'member' AND u.id = dm.peer_id
			LEFT JOIN agent a ON dm.peer_type = 'agent' AND a.id = dm.peer_id
			WHERE dm.peer_id IS NOT NULL
			ORDER BY dm.peer_id, dm.peer_type, dm.created_at DESC
		)
		SELECT peer_id, peer_type, peer_name, id, content, created_at, unread_count
		FROM latest
		ORDER BY created_at DESC
	`, parseUUID(workspaceID), parseUUID(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]map[string]any, 0)
	for rows.Next() {
		var (
			peerID      pgtype.UUID
			peerType    string
			peerName    pgtype.Text
			messageID   pgtype.UUID
			content     string
			createdAt   pgtype.Timestamptz
			unreadCount int64
		)
		if err := rows.Scan(&peerID, &peerType, &peerName, &messageID, &content, &createdAt, &unreadCount); err != nil {
			return nil, err
		}
		if !peerID.Valid {
			continue
		}
		displayName := uuidToString(peerID)
		if peerName.Valid && strings.TrimSpace(peerName.String) != "" {
			displayName = peerName.String
		}
		conversations = append(conversations, map[string]any{
			"peer_id":   uuidToString(peerID),
			"peer_type": peerType,
			"peer_name": displayName,
			"last_message": map[string]any{
				"id":         uuidToString(messageID),
				"content":    content,
				"created_at": timestampToString(createdAt),
			},
			"unread_count": unreadCount,
		})
	}
	return conversations, rows.Err()
}

func (h *Handler) GetWorkspaceSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	workspace, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: get workspace failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	agents, err := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list agents failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load agents")
		return
	}

	channels, err := h.Queries.ListChannels(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list channels failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load channels")
		return
	}

	files, err := h.Queries.ListFilesByWorkspace(r.Context(), db.ListFilesByWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		LimitVal:    200,
		OffsetVal:   0,
	})
	if err != nil {
		slog.Error("workspace snapshot: list files failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load files")
		return
	}

	tabs, err := h.Queries.ListBrowserTabs(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list browser tabs failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load browser tabs")
		return
	}

	contexts, err := h.Queries.ListBrowserContexts(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list browser contexts failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load browser contexts")
		return
	}

	collaborators, err := h.Queries.ListWorkspaceCollaborators(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list collaborators failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load collaborators")
		return
	}

	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace snapshot: list runtimes failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return
	}

	unreadCount, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		slog.Error("workspace snapshot: count unread inbox failed", "workspace_id", workspaceID, "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load inbox state")
		return
	}

	conversations, err := h.listDMConversations(r.Context(), workspaceID, userID)
	if err != nil && err != pgx.ErrNoRows {
		slog.Error("workspace snapshot: list dm conversations failed", "workspace_id", workspaceID, "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load conversations")
		return
	}

	agentResp := make([]AgentResponse, len(agents))
	for i, agent := range agents {
		agentResp[i] = agentToResponse(agent)
	}

	channelResp := make([]map[string]any, len(channels))
	for i, channel := range channels {
		channelResp[i] = channelToResponse(channel)
	}

	fileResp := make([]FileIndexResponse, len(files))
	for i, file := range files {
		fileResp[i] = fileIndexToResponse(file)
	}

	tabResp := make([]BrowserTabResponse, len(tabs))
	for i, tab := range tabs {
		tabResp[i] = browserTabToResponse(tab)
	}

	contextResp := make([]BrowserContextResponse, len(contexts))
	for i, item := range contexts {
		contextResp[i] = browserContextToResponse(item)
	}

	collaboratorResp := make([]WorkspaceCollaboratorResponse, len(collaborators))
	for i, collaborator := range collaborators {
		collaboratorResp[i] = collaboratorToResponse(collaborator)
	}

	runtimeResp := make([]AgentRuntimeResponse, len(runtimes))
	for i, runtime := range runtimes {
		runtimeResp[i] = runtimeToResponse(runtime)
	}

	workspaceResp, err := workspaceToResponse(workspace)
	if err != nil {
		writeWorkspaceResponseError(w, r, uuidToString(workspace.ID), err)
		return
	}

	writeJSON(w, http.StatusOK, WorkspaceSnapshotResponse{
		Workspace:       workspaceResp,
		Agents:          agentResp,
		Conversations:   conversations,
		Channels:        channelResp,
		Files:           fileResp,
		BrowserTabs:     tabResp,
		BrowserContexts: contextResp,
		Collaborators:   collaboratorResp,
		Inbox: map[string]any{
			"unread_count": unreadCount,
		},
		Runtimes: runtimeResp,
	})
}

func domainFromURL(rawURL string) *string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return nil
	}
	host := parsed.Hostname()
	if host == "" {
		return nil
	}
	return &host
}

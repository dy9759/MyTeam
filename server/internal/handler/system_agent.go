package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const systemAgentReturningColumns = `
	id, workspace_id, name, avatar_url, visibility, status,
	max_concurrent_tasks, owner_id, created_at, updated_at, description,
	runtime_id, instructions, archived_at, archived_by,
	auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
	trigger_on_channel_mention, needs_attention, needs_attention_reason,
	agent_type, identity_card, last_active_at, scope, owner_type`

type SystemAgentRequest struct {
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	Instructions       *string `json:"instructions"`
	AvatarURL          *string `json:"avatar_url"`
	RuntimeID          *string `json:"runtime_id"`
	Visibility         *string `json:"visibility"`
	Status             *string `json:"status"`
	Scope              *string `json:"scope"`
	MaxConcurrentTasks *int32  `json:"max_concurrent_tasks"`
}

// CreateSystemAgent — POST /api/system-agents
// Creates a workspace system agent. Admin or owner required.
func (h *Handler) CreateSystemAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}

	var req SystemAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	scope, _, err := systemAgentScope(req.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	runtimeID, ok := h.resolveSystemAgentRuntime(w, r, workspaceID, req.RuntimeID)
	if !ok {
		return
	}

	name := stringValue(req.Name, "System Agent")
	description := stringValue(req.Description, "Workspace system agent - manages defaults and automation")
	instructions := stringValue(req.Instructions, "")
	visibility := stringValue(req.Visibility, "workspace")
	status := stringValue(req.Status, "idle")
	maxConcurrentTasks := int32(1)
	if req.MaxConcurrentTasks != nil && *req.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = *req.MaxConcurrentTasks
	}

	agent, err := scanSystemAgentRow(h.DB.QueryRow(r.Context(), `
		INSERT INTO agent (
			workspace_id, name, description, avatar_url,
			runtime_id, visibility, status, max_concurrent_tasks,
			instructions, agent_type, owner_type, scope
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, 'system_agent', 'organization', $10
		)
		RETURNING `+systemAgentReturningColumns,
		parseUUID(workspaceID),
		name,
		description,
		ptrToText(req.AvatarURL),
		runtimeID,
		visibility,
		status,
		maxConcurrentTasks,
		instructions,
		scope,
	))
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "system agent already exists")
			return
		}
		slog.Warn("create system agent failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to create system agent")
		return
	}

	resp := agentToResponse(agent)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("agent:created", workspaceID, actorType, actorID, map[string]any{
		"agent":     resp,
		"is_system": true,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateSystemAgent — PATCH /api/system-agents/{id}
// Updates a workspace system agent. Admin or owner required.
func (h *Handler) UpdateSystemAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	var req SystemAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	scope, scopeProvided, err := systemAgentScope(req.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var runtimeID pgtype.UUID
	if req.RuntimeID != nil {
		var ok bool
		runtimeID, ok = h.resolveSystemAgentRuntime(w, r, workspaceID, req.RuntimeID)
		if !ok {
			return
		}
	}

	maxConcurrentTasks := pgtype.Int4{}
	if req.MaxConcurrentTasks != nil {
		maxConcurrentTasks = int4Of(*req.MaxConcurrentTasks)
	}

	agent, err := scanSystemAgentRow(h.DB.QueryRow(r.Context(), `
		UPDATE agent SET
			name = COALESCE($3, name),
			description = COALESCE($4, description),
			avatar_url = COALESCE($5, avatar_url),
			runtime_id = COALESCE($6, runtime_id),
			visibility = COALESCE($7, visibility),
			status = COALESCE($8, status),
			max_concurrent_tasks = COALESCE($9, max_concurrent_tasks),
			instructions = COALESCE($10, instructions),
			scope = CASE WHEN $11::boolean THEN $12::text ELSE scope END,
			updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND agent_type = 'system_agent'
		RETURNING `+systemAgentReturningColumns,
		parseUUID(id),
		parseUUID(workspaceID),
		ptrToText(req.Name),
		ptrToText(req.Description),
		ptrToText(req.AvatarURL),
		runtimeID,
		ptrToText(req.Visibility),
		ptrToText(req.Status),
		maxConcurrentTasks,
		ptrToText(req.Instructions),
		scopeProvided,
		scope,
	))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "system agent not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "system agent already exists")
			return
		}
		slog.Warn("update system agent failed", "error", err, "agent_id", id)
		writeError(w, http.StatusInternalServerError, "failed to update system agent")
		return
	}

	resp := agentToResponse(agent)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("agent:status", workspaceID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

// ArchiveSystemAgent — DELETE /api/system-agents/{id}
// Archives a workspace system agent. Owner required.
func (h *Handler) ArchiveSystemAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireWorkspaceOwner(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil || agent.AgentType != "system_agent" {
		writeError(w, http.StatusNotFound, "system agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "system agent is already archived")
		return
	}

	userID := requestUserID(r)
	archived, err := h.Queries.ArchiveAgent(r.Context(), db.ArchiveAgentParams{
		ID:         parseUUID(id),
		ArchivedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("archive system agent failed", "error", err, "agent_id", id)
		writeError(w, http.StatusInternalServerError, "failed to archive system agent")
		return
	}

	if err := h.Queries.CancelAgentTasksByAgent(r.Context(), parseUUID(id)); err != nil {
		slog.Warn("cancel system agent tasks on archive failed", "error", err, "agent_id", id)
	}

	resp := agentToResponse(archived)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("agent:archived", workspaceID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

// GetOrCreateSystemAgent — GET /api/system-agent
// Returns the workspace system agent, creating one if it doesn't exist.
// Also ensures the page system agents are present for the workspace
// and a personal agent exists for the current user.
func (h *Handler) GetOrCreateSystemAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	wsUUID := parseUUID(workspaceID)
	ownerUUID := parseUUID(userID)

	// Ensure personal agent exists for the user (fire-and-forget).
	go func() {
		user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
		if err != nil {
			return
		}
		if _, err := service.EnsurePersonalAgent(r.Context(), h.Queries, wsUUID, ownerUUID, user.Name); err != nil {
			slog.Debug("ensure personal agent failed", "error", err)
		}
	}()

	// Try to get existing
	agent, err := h.Queries.GetSystemAgent(r.Context(), wsUUID)
	if err == nil {
		service.EnsurePageAgents(r.Context(), h.Queries, wsUUID, ownerUUID)
		writeJSON(w, http.StatusOK, agentToResponse(agent))
		return
	}

	// Ensure cloud runtime exists for this workspace (needed as FK on agent row).
	cloudRuntime, rterr := h.Queries.EnsureCloudRuntime(r.Context(), wsUUID)
	if rterr != nil {
		slog.Warn("ensure cloud runtime failed", "error", rterr)
		writeError(w, http.StatusInternalServerError, "failed to ensure cloud runtime")
		return
	}

	// Create system agent. owner_id is NULL on system agents per the
	// agent_type_owner_match constraint introduced in migration 050.
	_ = ownerUUID
	agent, err = h.Queries.CreateSystemAgent(r.Context(), db.CreateSystemAgentParams{
		WorkspaceID: wsUUID,
		RuntimeID:   cloudRuntime.ID,
	})
	if err != nil {
		slog.Warn("create system agent failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create system agent")
		return
	}

	// Cloud LLM config now lives on the runtime, not the agent. Persist a snapshot
	// of the server-side env config under runtime.metadata.cloud_llm_config so the
	// cloud executor can pick it up when the agent dispatches a task.
	llmCfg := service.LoadCloudLLMConfigFromEnv()
	if llmJSON, err := json.Marshal(llmCfg); err == nil {
		if err := h.Queries.SetRuntimeMetadataKey(r.Context(), db.SetRuntimeMetadataKeyParams{
			ID:    cloudRuntime.ID,
			Key:   "cloud_llm_config",
			Value: llmJSON,
		}); err != nil {
			slog.Warn("persist runtime cloud_llm_config failed", "error", err, "runtime_id", uuidToString(cloudRuntime.ID))
		}
	}

	service.EnsurePageAgents(r.Context(), h.Queries, wsUUID, ownerUUID)

	h.publish("agent:created", workspaceID, "system", userID, map[string]any{
		"agent":     agentToResponse(agent),
		"is_system": true,
	})

	writeJSON(w, http.StatusCreated, agentToResponse(agent))
}

// ListPageAgents — GET /api/page-agents
// Returns the page system agents for the current workspace.
func (h *Handler) ListPageAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	agents, err := h.Queries.ListPageSystemAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list page agents failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list page agents")
		return
	}

	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetPageAgent — GET /api/page-agents/{scope}
// Returns a single page system agent for the given scope.
func (h *Handler) GetPageAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	scope := chi.URLParam(r, "scope")
	if !isValidScope(scope) {
		writeError(w, http.StatusBadRequest, "invalid page scope")
		return
	}

	agent, err := h.Queries.GetPageSystemAgent(r.Context(), db.GetPageSystemAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		Scope:       textOf(scope),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "page agent not found")
			return
		}
		slog.Warn("get page agent failed", "error", err, "scope", scope)
		writeError(w, http.StatusInternalServerError, "failed to load page agent")
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
}

func isValidScope(s string) bool {
	switch s {
	case "account", "conversation", "project", "file":
		return true
	}
	return false
}

func stringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func systemAgentScope(value *string) (pgtype.Text, bool, error) {
	if value == nil {
		return pgtype.Text{}, false, nil
	}
	scope := strings.TrimSpace(*value)
	if scope == "" {
		return pgtype.Text{}, true, nil
	}
	if !isValidScope(scope) {
		return pgtype.Text{}, true, errors.New("invalid page scope")
	}
	return textOf(scope), true, nil
}

func (h *Handler) resolveSystemAgentRuntime(w http.ResponseWriter, r *http.Request, workspaceID string, runtimeID *string) (pgtype.UUID, bool) {
	if runtimeID == nil || strings.TrimSpace(*runtimeID) == "" {
		runtime, err := h.Queries.EnsureCloudRuntime(r.Context(), parseUUID(workspaceID))
		if err != nil {
			slog.Warn("ensure system agent runtime failed", "error", err, "workspace_id", workspaceID)
			writeError(w, http.StatusInternalServerError, "failed to ensure runtime")
			return pgtype.UUID{}, false
		}
		return runtime.ID, true
	}

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(strings.TrimSpace(*runtimeID)),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return pgtype.UUID{}, false
	}
	return runtime.ID, true
}

func scanSystemAgentRow(row pgx.Row) (db.Agent, error) {
	var agent db.Agent
	err := row.Scan(
		&agent.ID,
		&agent.WorkspaceID,
		&agent.Name,
		&agent.AvatarUrl,
		&agent.Visibility,
		&agent.Status,
		&agent.MaxConcurrentTasks,
		&agent.OwnerID,
		&agent.CreatedAt,
		&agent.UpdatedAt,
		&agent.Description,
		&agent.RuntimeID,
		&agent.Instructions,
		&agent.ArchivedAt,
		&agent.ArchivedBy,
		&agent.AutoReplyEnabled,
		&agent.AutoReplyConfig,
		&agent.DisplayName,
		&agent.Avatar,
		&agent.Bio,
		&agent.Tags,
		&agent.TriggerOnChannelMention,
		&agent.NeedsAttention,
		&agent.NeedsAttentionReason,
		&agent.AgentType,
		&agent.IdentityCard,
		&agent.LastActiveAt,
		&agent.Scope,
		&agent.OwnerType,
	)
	return agent, err
}

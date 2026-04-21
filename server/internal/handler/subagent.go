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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SubagentResponse is the JSON shape returned by /api/subagents. It
// intentionally omits the heavier agent fields (runtime config, status,
// tasks) because subagents are templates, not runnable agents — they
// only carry metadata + a skill roster.
type SubagentResponse struct {
	ID          string          `json:"id"`
	WorkspaceID *string         `json:"workspace_id"` // nil for globals
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	IsGlobal    bool            `json:"is_global"`
	Source      string          `json:"source"`
	SourceRef   *string         `json:"source_ref,omitempty"`
	Instructions string         `json:"instructions"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Skills      []SkillResponse `json:"skills,omitempty"`
}

func subagentRowToResponse(r db.ListSubagentsRow, skills []db.Skill) SubagentResponse {
	resp := SubagentResponse{
		ID:           uuidToString(r.ID),
		Name:         r.Name,
		Description:  r.Description,
		Category:     r.Category,
		IsGlobal:     r.IsGlobal,
		Source:       r.Source,
		SourceRef:    textToPtr(r.SourceRef),
		Instructions: r.Instructions,
		CreatedAt:    timestampToString(r.CreatedAt),
		UpdatedAt:    timestampToString(r.UpdatedAt),
	}
	if r.WorkspaceID.Valid {
		s := uuidToString(r.WorkspaceID)
		resp.WorkspaceID = &s
	}
	if len(skills) > 0 {
		resp.Skills = make([]SkillResponse, len(skills))
		for i, s := range skills {
			resp.Skills[i] = skillToResponse(s)
		}
	}
	return resp
}

// subagentGetRowToResponse mirrors subagentRowToResponse for GetSubagent
// — sqlc emits a separate Row type per query even when the columns are
// identical, so we need a thin adapter rather than reusing the function.
func subagentGetRowToResponse(r db.GetSubagentRow, skills []db.Skill) SubagentResponse {
	return subagentRowToResponse(db.ListSubagentsRow(r), skills)
}

// ---------- Handlers ----------

// ListSubagents handles GET /api/subagents?category=&scope=
// scope = "global" | "workspace" | "all" (default "all")
func (h *Handler) ListSubagents(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope == "" {
		scope = "all"
	}

	params := db.ListSubagentsParams{
		Category: textFromQuery(r, "category"),
	}
	// "global" → pass NULL workspace so only is_global rows come back;
	// "workspace" or "all" → pass the current workspace UUID so both
	// globals and workspace rows show up (the SQL union already merges).
	if scope != "global" && workspaceID != "" {
		params.WorkspaceID = parseUUID(workspaceID)
	}

	rows, err := h.Queries.ListSubagents(r.Context(), params)
	if err != nil {
		slog.Error("list subagents failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list subagents")
		return
	}

	if scope == "workspace" {
		// Caller asked for workspace-only — strip globals that the SQL
		// union always includes.
		filtered := rows[:0]
		for _, row := range rows {
			if !row.IsGlobal {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	resp := make([]SubagentResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, subagentRowToResponse(row, nil))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetSubagent handles GET /api/subagents/{id} and includes the linked
// skill roster so the UI doesn't need a second round-trip.
func (h *Handler) GetSubagent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	row, err := h.Queries.GetSubagent(r.Context(), parseUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		slog.Error("get subagent failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "failed to load subagent")
		return
	}

	// Workspace scoping — globals are always readable; workspace rows
	// require the caller be in that workspace.
	if !row.IsGlobal {
		workspaceID := resolveWorkspaceID(r)
		if workspaceID == "" || uuidToString(row.WorkspaceID) != workspaceID {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
	}

	skills, err := h.Queries.ListSubagentSkills(r.Context(), row.ID)
	if err != nil {
		slog.Error("list subagent skills failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load subagent skills")
		return
	}

	writeJSON(w, http.StatusOK, subagentGetRowToResponse(row, skills))
}

// CreateSubagent handles POST /api/subagents — workspace-scoped only.
// Globals come from the bundle loader, never the API.
type CreateSubagentRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Category     string `json:"category"`
}

func (h *Handler) CreateSubagent(w http.ResponseWriter, r *http.Request) {
	var req CreateSubagentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	row, err := h.Queries.CreateWorkspaceSubagent(r.Context(), db.CreateWorkspaceSubagentParams{
		WorkspaceID:  parseUUID(workspaceID),
		Name:         req.Name,
		Description:  req.Description,
		Instructions: req.Instructions,
		Category:     req.Category,
		OwnerID:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("create subagent failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create subagent")
		return
	}

	// CreateWorkspaceSubagent has the same columns as ListSubagents so
	// the shape adapters compose cleanly.
	resp := subagentRowToResponse(db.ListSubagentsRow(row), nil)
	h.publish(protocol.EventSubagentCreated, workspaceID, "member", userID,
		map[string]any{"subagent": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateSubagent handles PATCH /api/subagents/{id}. Global/bundle rows
// are read-only — updates to them are rejected so resyncs can't be
// silently overridden.
type UpdateSubagentRequest struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Instructions *string `json:"instructions"`
	Category     *string `json:"category"`
}

func (h *Handler) UpdateSubagent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	var req UpdateSubagentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	current, err := h.Queries.GetSubagent(r.Context(), parseUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		slog.Error("update subagent: load failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load subagent")
		return
	}
	if current.Source == "bundle" {
		writeError(w, http.StatusConflict, "bundle subagents are read-only")
		return
	}
	if workspaceID == "" || uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}

	params := db.UpdateSubagentParams{ID: current.ID}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Instructions != nil {
		params.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	if req.Category != nil {
		params.Category = pgtype.Text{String: *req.Category, Valid: true}
	}

	updated, err := h.Queries.UpdateSubagent(r.Context(), params)
	if err != nil {
		slog.Error("update subagent failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update subagent")
		return
	}

	resp := subagentRowToResponse(db.ListSubagentsRow(updated), nil)
	h.publish(protocol.EventSubagentUpdated, workspaceID, "member", userID,
		map[string]any{"subagent": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteSubagent handles DELETE /api/subagents/{id}. Bundle rows are
// rejected at the SQL layer (`source <> 'bundle'` in DeleteSubagent),
// so this layer only guards workspace scoping.
func (h *Handler) DeleteSubagent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	current, err := h.Queries.GetSubagent(r.Context(), parseUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "subagent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load subagent")
		return
	}
	if current.Source == "bundle" {
		writeError(w, http.StatusConflict, "bundle subagents are read-only")
		return
	}
	if workspaceID == "" || uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}

	if err := h.Queries.DeleteSubagent(r.Context(), current.ID); err != nil {
		slog.Error("delete subagent failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete subagent")
		return
	}

	h.publish(protocol.EventSubagentDeleted, workspaceID, "member", userID,
		map[string]any{"subagent_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// LinkSubagentSkill handles POST /api/subagents/{id}/skills
// body: {"skill_id": "<uuid>", "position": <int>}
type LinkSubagentSkillRequest struct {
	SkillID  string `json:"skill_id"`
	Position int32  `json:"position"`
}

func (h *Handler) LinkSubagentSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	var req LinkSubagentSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SkillID == "" {
		writeError(w, http.StatusBadRequest, "skill_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	current, err := h.Queries.GetSubagent(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}
	// Bundle subagents' roster is managed by the bundle loader. Reject
	// API-level link edits so user config doesn't fight the next resync.
	if current.Source == "bundle" {
		writeError(w, http.StatusConflict, "bundle subagents are read-only")
		return
	}
	if workspaceID == "" || uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}

	// Confirm the skill is reachable to this workspace (workspace-local
	// or global). GetSkillInWorkspace returns globals via the OR clause
	// added in migration 069.
	if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
		ID:          parseUUID(req.SkillID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	if err := h.Queries.LinkSubagentSkill(r.Context(), db.LinkSubagentSkillParams{
		SubagentID: current.ID,
		SkillID:    parseUUID(req.SkillID),
		Position:   req.Position,
	}); err != nil {
		slog.Error("link subagent skill failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to link skill")
		return
	}

	h.publish(protocol.EventSubagentSkillLinked, workspaceID, "member", userID, map[string]any{
		"subagent_id": id,
		"skill_id":    req.SkillID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "linked"})
}

// UnlinkSubagentSkill handles DELETE /api/subagents/{id}/skills/{skillID}
func (h *Handler) UnlinkSubagentSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skillID := chi.URLParam(r, "skillID")
	if id == "" || skillID == "" {
		writeError(w, http.StatusBadRequest, "id and skillID are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	current, err := h.Queries.GetSubagent(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}
	if current.Source == "bundle" {
		writeError(w, http.StatusConflict, "bundle subagents are read-only")
		return
	}
	if workspaceID == "" || uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "subagent not found")
		return
	}

	if err := h.Queries.UnlinkSubagentSkill(r.Context(), db.UnlinkSubagentSkillParams{
		SubagentID: current.ID,
		SkillID:    parseUUID(skillID),
	}); err != nil {
		slog.Error("unlink subagent skill failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unlink skill")
		return
	}

	h.publish(protocol.EventSubagentSkillUnlinked, workspaceID, "member", userID, map[string]any{
		"subagent_id": id,
		"skill_id":    skillID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

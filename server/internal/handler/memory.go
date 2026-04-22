package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
)

type createMemoryRequest struct {
	Type       string   `json:"type"`
	Scope      string   `json:"scope"`
	Source     string   `json:"source"`
	RawKind    string   `json:"raw_kind"`
	RawID      string   `json:"raw_id"`
	Summary    string   `json:"summary,omitempty"`
	Body       string   `json:"body,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Entities   []string `json:"entities,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
}

type searchMemoriesRequest struct {
	Query  string   `json:"query"`
	TopK   int      `json:"top_k,omitempty"`
	Type   string   `json:"type,omitempty"`
	Scope  string   `json:"scope,omitempty"`
	Status []string `json:"status,omitempty"`
}

type memorySearchHitResponse struct {
	Chunk memory.Chunk `json:"chunk"`
	Score float64      `json:"score"`
}

func (h *Handler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	memorySvc, ok := h.requireMemoryService(w)
	if !ok {
		return
	}
	wsUUID, ok := parseRequestUUID(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userUUID, ok := parseRequestUUID(w, userID, "user id")
	if !ok {
		return
	}

	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if req.Scope == "" {
		writeError(w, http.StatusBadRequest, "scope is required")
		return
	}
	if req.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}
	if req.RawKind == "" {
		writeError(w, http.StatusBadRequest, "raw_kind is required")
		return
	}
	rawID, ok := parseRequestUUID(w, req.RawID, "raw_id")
	if !ok {
		return
	}

	mem, err := memorySvc.Append(r.Context(), memory.AppendInput{
		WorkspaceID: wsUUID,
		CreatedBy:   userUUID,
		Type:        memory.MemoryType(req.Type),
		Scope:       memory.MemoryScope(req.Scope),
		Source:      req.Source,
		Raw:         memory.RawRef{Kind: memory.RawKind(req.RawKind), ID: rawID},
		Summary:     req.Summary,
		Body:        req.Body,
		Tags:        req.Tags,
		Entities:    req.Entities,
		Confidence:  req.Confidence,
	})
	if err != nil {
		if errors.Is(err, memory.ErrInvalidRaw) {
			writeError(w, http.StatusBadRequest, "invalid_raw")
			return
		}
		slog.Error("memory create failed", "workspace_id", workspaceID, "err", err)
		writeError(w, http.StatusInternalServerError, "create memory failed")
		return
	}

	writeJSON(w, http.StatusCreated, mem)
}

func (h *Handler) ListMemories(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	memorySvc, ok := h.requireMemoryService(w)
	if !ok {
		return
	}
	wsUUID, ok := parseRequestUUID(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	memories, err := memorySvc.ListByWorkspace(r.Context(), wsUUID, memory.ListFilter{
		Type:   memory.MemoryType(r.URL.Query().Get("type")),
		Scope:  memory.MemoryScope(r.URL.Query().Get("scope")),
		Status: memory.MemoryStatus(r.URL.Query().Get("status")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		slog.Error("memory list failed", "workspace_id", workspaceID, "err", err)
		writeError(w, http.StatusInternalServerError, "list memories failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"memories": memories})
}

func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	memorySvc, ok := h.requireMemoryService(w)
	if !ok {
		return
	}
	wsUUID, ok := parseRequestUUID(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req searchMemoriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	var types []memory.MemoryType
	if req.Type != "" {
		types = []memory.MemoryType{memory.MemoryType(req.Type)}
	}
	var scopes []memory.MemoryScope
	if req.Scope != "" {
		scopes = []memory.MemoryScope{memory.MemoryScope(req.Scope)}
	}
	statuses := make([]memory.MemoryStatus, len(req.Status))
	for i, status := range req.Status {
		statuses[i] = memory.MemoryStatus(status)
	}

	hits, err := memorySvc.Search(r.Context(), memory.SearchInput{
		WorkspaceID: wsUUID,
		Query:       req.Query,
		TopK:        req.TopK,
		Types:       types,
		Scopes:      scopes,
		StatusOnly:  statuses,
	})
	if err != nil {
		if errors.Is(err, memory.ErrIndexingNotWired) {
			writeError(w, http.StatusServiceUnavailable, "indexing_not_wired")
			return
		}
		slog.Error("memory search failed", "workspace_id", workspaceID, "err", err)
		writeError(w, http.StatusInternalServerError, "search memories failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"hits": memoryHitsToResponse(hits)})
}

func (h *Handler) PromoteMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	memorySvc, ok := h.requireMemoryService(w)
	if !ok {
		return
	}
	if _, ok := parseRequestUUID(w, workspaceID, "workspace id"); !ok {
		return
	}
	memoryID, ok := parseRequestUUID(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	mem, err := memorySvc.Promote(r.Context(), memoryID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		slog.Error("memory promote failed", "workspace_id", workspaceID, "memory_id", memoryID, "err", err)
		writeError(w, http.StatusInternalServerError, "promote memory failed")
		return
	}

	writeJSON(w, http.StatusOK, mem)
}

func (h *Handler) requireMemoryService(w http.ResponseWriter) (*memory.Service, bool) {
	if h.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, "memory_not_wired")
		return nil, false
	}
	return h.Memory, true
}

func parseRequestUUID(w http.ResponseWriter, value, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func memoryHitsToResponse(hits []memory.Hit) []memorySearchHitResponse {
	out := make([]memorySearchHitResponse, len(hits))
	for i, hit := range hits {
		out[i] = memorySearchHitResponse{
			Chunk: hit.Chunk,
			Score: hit.Score,
		}
	}
	return out
}

package handler

import (
	"encoding/json"
	"net/http"

	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	"github.com/go-chi/chi/v5"
)

func (h *Handler) ClaimConversationAgentRun(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	run, err := h.ConversationRuns.ClaimNext(r.Context(), runtimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

func (h *Handler) StartConversationAgentRun(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	if err := h.ConversationRuns.MarkRunning(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type conversationRunEventsRequest struct {
	Events []service.ConversationAgentRunEventInput `json:"events"`
}

func (h *Handler) ReportConversationAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	var req conversationRunEventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.ConversationRuns.AppendEvents(r.Context(), chi.URLParam(r, "id"), req.Events); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type conversationRunCompleteRequest struct {
	Output    string `json:"output"`
	SessionID string `json:"session_id"`
	WorkDir   string `json:"work_dir"`
}

func (h *Handler) CompleteConversationAgentRun(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	var req conversationRunCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.ConversationRuns.Complete(r.Context(), chi.URLParam(r, "id"), req.Output, req.SessionID, req.WorkDir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type conversationRunFailRequest struct {
	Error string `json:"error"`
}

func (h *Handler) FailConversationAgentRun(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	var req conversationRunFailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.ConversationRuns.Fail(r.Context(), chi.URLParam(r, "id"), req.Error); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) GetConversationAgentRunStatus(w http.ResponseWriter, r *http.Request) {
	if h.ConversationRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "conversation agent run service is not configured")
		return
	}
	status, err := h.ConversationRuns.Status(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

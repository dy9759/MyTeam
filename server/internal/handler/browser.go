package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) ListBrowserTabs(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	tabs, err := h.Queries.ListBrowserTabs(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list browser tabs")
		return
	}

	resp := make([]BrowserTabResponse, len(tabs))
	for i, tab := range tabs {
		resp[i] = browserTabToResponse(tab)
	}

	writeJSON(w, http.StatusOK, map[string]any{"tabs": resp})
}

func (h *Handler) CreateBrowserTab(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req struct {
		URL            string   `json:"url"`
		Title          string   `json:"title"`
		SharedWith     []string `json:"shared_with"`
		ContextID      *string  `json:"context_id"`
		ConversationID *string  `json:"conversation_id"`
		ProjectID      *string  `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tab, err := h.Queries.CreateBrowserTab(r.Context(), db.CreateBrowserTabParams{
		WorkspaceID:    parseUUID(workspaceID),
		Url:            strings.TrimSpace(defaultString(req.URL, "about:blank")),
		Title:          strToText(strings.TrimSpace(req.Title)),
		Status:         "active",
		CreatedBy:      "member:" + userID,
		SharedWith:     mustMarshalJSON(req.SharedWith),
		ContextID:      ptrToUUID(req.ContextID),
		SessionID:      pgText("session-" + fmt.Sprint(time.Now().UnixNano())),
		LiveUrl:        pgtype.Text{},
		ScreenshotUrl:  pgtype.Text{},
		ConversationID: ptrToUUID(req.ConversationID),
		ProjectID:      ptrToUUID(req.ProjectID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create browser tab")
		return
	}

	writeJSON(w, http.StatusCreated, browserTabToResponse(tab))
}

func (h *Handler) ReconnectBrowserTab(w http.ResponseWriter, r *http.Request) {
	tabID := chi.URLParam(r, "tabId")
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "tabId is required")
		return
	}

	tab, err := h.Queries.ReconnectBrowserTab(r.Context(), db.ReconnectBrowserTabParams{
		ID:            parseUUID(tabID),
		SessionID:     pgText("session-" + fmt.Sprint(time.Now().UnixNano())),
		LiveUrl:       pgtype.Text{},
		ScreenshotUrl: pgtype.Text{},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reconnect browser tab")
		return
	}

	writeJSON(w, http.StatusOK, browserTabToResponse(tab))
}

func (h *Handler) PersistBrowserTab(w http.ResponseWriter, r *http.Request) {
	tabID := chi.URLParam(r, "tabId")
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "tabId is required")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	tab, err := h.Queries.GetBrowserTab(r.Context(), parseUUID(tabID))
	if err != nil {
		writeError(w, http.StatusNotFound, "browser tab not found")
		return
	}

	if tab.ContextID.Valid {
		existing, err := h.Queries.GetBrowserContext(r.Context(), tab.ContextID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load browser context")
			return
		}
		writeJSON(w, http.StatusOK, browserContextToResponse(existing))
		return
	}

	domain := domainFromURL(tab.Url)
	created, err := h.Queries.CreateBrowserContext(r.Context(), db.CreateBrowserContextParams{
		WorkspaceID: tab.WorkspaceID,
		Name:        name,
		Domain:      ptrToText(domain),
		Status:      "active",
		CreatedBy:   tab.CreatedBy,
		SharedWith:  tab.SharedWith,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "browser context name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create browser context")
		return
	}

	if _, err := h.Queries.AttachBrowserTabContext(r.Context(), db.AttachBrowserTabContextParams{
		ContextID: created.ID,
		ID:        tab.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach browser context")
		return
	}

	writeJSON(w, http.StatusOK, browserContextToResponse(created))
}

func (h *Handler) UnpersistBrowserTab(w http.ResponseWriter, r *http.Request) {
	tabID := chi.URLParam(r, "tabId")
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "tabId is required")
		return
	}

	tab, err := h.Queries.ClearBrowserTabContext(r.Context(), parseUUID(tabID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear browser context")
		return
	}

	writeJSON(w, http.StatusOK, browserTabToResponse(tab))
}

func (h *Handler) DeleteBrowserTab(w http.ResponseWriter, r *http.Request) {
	tabID := chi.URLParam(r, "tabId")
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "tabId is required")
		return
	}

	if err := h.Queries.CloseBrowserTab(r.Context(), parseUUID(tabID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close browser tab")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateBrowserTabSharing(w http.ResponseWriter, r *http.Request) {
	tabID := chi.URLParam(r, "tabId")
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "tabId is required")
		return
	}

	var req struct {
		SharedWith []string `json:"shared_with"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tab, err := h.Queries.UpdateBrowserTabSharing(r.Context(), db.UpdateBrowserTabSharingParams{
		ID:         parseUUID(tabID),
		SharedWith: mustMarshalJSON(req.SharedWith),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update browser tab sharing")
		return
	}

	writeJSON(w, http.StatusOK, browserTabToResponse(tab))
}

func (h *Handler) ListBrowserContexts(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	contexts, err := h.Queries.ListBrowserContexts(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list browser contexts")
		return
	}

	resp := make([]BrowserContextResponse, len(contexts))
	for i, item := range contexts {
		resp[i] = browserContextToResponse(item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"contexts": resp})
}

func (h *Handler) DeleteBrowserContext(w http.ResponseWriter, r *http.Request) {
	contextID := chi.URLParam(r, "contextId")
	if contextID == "" {
		writeError(w, http.StatusBadRequest, "contextId is required")
		return
	}

	if err := h.Queries.DeleteBrowserContext(r.Context(), parseUUID(contextID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete browser context")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func mustMarshalJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte("[]")
	}
	return data
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

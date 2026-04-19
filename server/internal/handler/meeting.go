// Package handler: meeting.go — HTTP surface for the meeting lifecycle.
// Each endpoint constructs a per-request MeetingService bound to the
// workspace's storage backend (TOS or fallback S3) so multi-tenant
// uploads land in the right bucket.
//
// Endpoints (all protected by RequireWorkspaceMember at the router):
//
//	POST   /api/threads/{threadID}/meeting/start          {agenda?: []string}
//	POST   /api/threads/{threadID}/meeting/audio          multipart file=...
//	POST   /api/threads/{threadID}/meeting/summarize      {audio_url?: string}
//	GET    /api/threads/{threadID}/meeting/action-items
//	POST   /api/action-items/{itemID}/approve             {plan_id, agent_id}
//	POST   /api/action-items/{itemID}/reject
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
)

// maxAudioUploadMem caps the in-memory multipart parsing buffer at
// 32 MiB; larger parts spill to a temp file via Go's mime/multipart.
const maxAudioUploadMem = 32 << 20

// maxAudioUploadBytes is the hard total cap on the request body. A
// 60-minute mp3 at 128kbps ≈ 60 MiB; 500 MiB headroom covers
// uncompressed wav fallbacks. Wraps the body in http.MaxBytesReader so
// a malicious client can't fill the temp dir. Issue #62.
const maxAudioUploadBytes = 500 << 20

// allowedAudioMIMEs gates Content-Type at the IPC boundary so a
// renamed `.mp3` containing executable bytes can't get forwarded to
// ASR. Issue #62.
var allowedAudioMIMEs = map[string]bool{
	"audio/mpeg":  true, // mp3
	"audio/mp4":   true, // m4a
	"audio/wav":   true,
	"audio/x-wav": true,
	"audio/webm":  true,
	"audio/ogg":   true,
	"audio/flac":  true,
	"audio/aac":   true,
}

// isAllowedAudioMIME accepts only the well-known audio/* types we
// know Doubao 妙记 can transcribe; rejects anything else (including
// bare "audio/" since some browsers send empty subtypes).
func isAllowedAudioMIME(ct string) bool {
	if ct == "" {
		return false
	}
	// Strip parameters like `; charset=binary`.
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	return allowedAudioMIMEs[ct]
}

type startMeetingRequest struct {
	Agenda []string `json:"agenda,omitempty"`
}

type summarizeRequest struct {
	AudioURL string `json:"audio_url,omitempty"`
}

type approveActionItemRequest struct {
	PlanID  string `json:"plan_id"`
	AgentID string `json:"agent_id"`
}

// meetingService builds a per-request MeetingService bound to the
// workspace's storage backend. Returns nil + writes 503 when deps not
// wired (Secrets/ASR missing in dev or test envs).
func (h *Handler) meetingService(w http.ResponseWriter, r *http.Request, workspaceID uuid.UUID) (*service.MeetingService, bool) {
	if h.Secrets == nil || h.ASR == nil {
		writeError(w, http.StatusServiceUnavailable, "meeting_not_wired")
		return nil, false
	}
	svc := service.NewMeetingService(h.Queries, h.TxStarter, h.Secrets, h.ASR)
	if h.Memory != nil {
		svc = svc.WithMemory(h.Memory)
	}
	if h.StorageFactory != nil {
		st, err := h.StorageFactory.NewFromWorkspace(r.Context(), workspaceID)
		if err == nil {
			svc = svc.WithStorage(st)
		} else if !errors.Is(err, storage.ErrNoBackend) {
			slog.Warn("meeting: storage factory error",
				"workspace_id", workspaceID, "err", err)
		}
	}
	return svc, true
}

// loadThreadForMember verifies the URL thread belongs to the request's
// workspace AND the requester is a member. Returns the thread + parsed
// uuid for downstream service calls.
func (h *Handler) loadThreadForMember(w http.ResponseWriter, r *http.Request, threadIDParam string) (uuid.UUID, uuid.UUID, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return uuid.Nil, uuid.Nil, false
	}
	threadID, ok := parseRequestUUID(w, threadIDParam, "thread_id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	thread, err := h.Queries.GetThread(r.Context(), parseUUID(threadID.String()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "thread not found")
			return uuid.Nil, uuid.Nil, false
		}
		slog.Error("meeting: get thread failed", "thread_id", threadID, "err", err)
		writeError(w, http.StatusInternalServerError, "load thread failed")
		return uuid.Nil, uuid.Nil, false
	}
	wsID := uuid.UUID(thread.WorkspaceID.Bytes)
	if requestWS := resolveWorkspaceID(r); requestWS != "" && requestWS != wsID.String() {
		writeError(w, http.StatusNotFound, "thread not found")
		return uuid.Nil, uuid.Nil, false
	}
	if _, ok := h.workspaceMember(w, r, wsID.String()); !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return threadID, wsID, true
}

// StartMeeting — POST /api/threads/{threadID}/meeting/start
func (h *Handler) StartMeeting(w http.ResponseWriter, r *http.Request) {
	threadID, wsID, ok := h.loadThreadForMember(w, r, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	var req startMeetingRequest
	// Empty body is OK — agenda is optional.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}

	meta, err := svc.StartMeeting(r.Context(), threadID, req.Agenda)
	if err != nil {
		slog.Error("meeting start failed", "thread_id", threadID, "err", err)
		writeError(w, http.StatusInternalServerError, "start meeting failed")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// UploadMeetingAudio — POST /api/threads/{threadID}/meeting/audio (multipart)
func (h *Handler) UploadMeetingAudio(w http.ResponseWriter, r *http.Request) {
	threadID, wsID, ok := h.loadThreadForMember(w, r, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	ownerID, ok := parseRequestUUID(w, userID, "user_id")
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	// Hard cap total request body — protects the temp dir from a
	// malicious client streaming infinite bytes. Issue #62.
	r.Body = http.MaxBytesReader(w, r.Body, maxAudioUploadBytes)

	if err := r.ParseMultipartForm(maxAudioUploadMem); err != nil {
		// MaxBytesReader returns its own error when the cap is hit;
		// either way the client gets 413/400 with a clear message.
		writeError(w, http.StatusRequestEntityTooLarge, "invalid multipart form or too large")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file part required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !isAllowedAudioMIME(contentType) {
		writeError(w, http.StatusUnsupportedMediaType,
			"file Content-Type must be a supported audio/* mime")
		return
	}
	meta, fileID, err := svc.UploadAudio(r.Context(), threadID, ownerID, file, header.Filename, contentType, header.Size)
	if err != nil {
		if errors.Is(err, service.ErrNotMeeting) {
			writeError(w, http.StatusBadRequest, "thread is not a meeting; call /start first")
			return
		}
		slog.Error("meeting upload audio failed", "thread_id", threadID, "err", err)
		writeError(w, http.StatusInternalServerError, "upload audio failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"meta":    meta,
		"file_id": fileID.String(),
	})
}

// SummarizeMeeting — POST /api/threads/{threadID}/meeting/summarize
func (h *Handler) SummarizeMeeting(w http.ResponseWriter, r *http.Request) {
	threadID, wsID, ok := h.loadThreadForMember(w, r, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	var req summarizeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}

	bundle, err := svc.Summarize(r.Context(), threadID, req.AudioURL)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotMeeting):
			writeError(w, http.StatusBadRequest, "thread is not a meeting; call /start first")
		case errors.Is(err, service.ErrAudioMissing):
			writeError(w, http.StatusBadRequest, "no audio attached and no audio_url provided")
		default:
			slog.Error("meeting summarize failed", "thread_id", threadID, "err", err)
			writeError(w, http.StatusInternalServerError, "summarize failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// ListMeetingActionItems — GET /api/threads/{threadID}/meeting/action-items
func (h *Handler) ListMeetingActionItems(w http.ResponseWriter, r *http.Request) {
	threadID, wsID, ok := h.loadThreadForMember(w, r, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	items, err := svc.ListActionItems(r.Context(), threadID)
	if err != nil {
		slog.Error("meeting list action items failed", "thread_id", threadID, "err", err)
		writeError(w, http.StatusInternalServerError, "list action items failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// loadActionItemForMember resolves /api/action-items/{itemID} → looks up
// the parent thread → verifies workspace membership. Returns parsed
// itemID + workspace UUID for the per-request MeetingService.
func (h *Handler) loadActionItemForMember(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return uuid.Nil, uuid.Nil, false
	}
	itemID, ok := parseRequestUUID(w, chi.URLParam(r, "itemID"), "itemID")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	item, err := h.Queries.GetThreadContextItem(r.Context(), parseUUID(itemID.String()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "action item not found")
			return uuid.Nil, uuid.Nil, false
		}
		slog.Error("meeting: get action item failed", "item_id", itemID, "err", err)
		writeError(w, http.StatusInternalServerError, "load action item failed")
		return uuid.Nil, uuid.Nil, false
	}
	wsID := uuid.UUID(item.WorkspaceID.Bytes)
	if _, ok := h.workspaceMember(w, r, wsID.String()); !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return itemID, wsID, true
}

// ApproveActionItem — POST /api/action-items/{itemID}/approve
func (h *Handler) ApproveActionItem(w http.ResponseWriter, r *http.Request) {
	itemID, wsID, ok := h.loadActionItemForMember(w, r)
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	var req approveActionItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	planID, ok := parseRequestUUID(w, req.PlanID, "plan_id")
	if !ok {
		return
	}
	agentID, ok := parseRequestUUID(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	task, err := svc.ApproveActionItem(r.Context(), itemID, planID, agentID)
	if err != nil {
		if errors.Is(err, service.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, "action item not found")
			return
		}
		slog.Error("meeting approve action item failed", "item_id", itemID, "err", err)
		writeError(w, http.StatusInternalServerError, "approve failed")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// RejectActionItem — POST /api/action-items/{itemID}/reject
func (h *Handler) RejectActionItem(w http.ResponseWriter, r *http.Request) {
	itemID, wsID, ok := h.loadActionItemForMember(w, r)
	if !ok {
		return
	}
	svc, ok := h.meetingService(w, r, wsID)
	if !ok {
		return
	}

	if err := svc.RejectActionItem(r.Context(), itemID); err != nil {
		if errors.Is(err, service.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, "action item not found")
			return
		}
		slog.Error("meeting reject action item failed", "item_id", itemID, "err", err)
		writeError(w, http.StatusInternalServerError, "reject failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

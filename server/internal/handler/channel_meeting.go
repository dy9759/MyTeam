// Package handler — channel_meeting.go.
//
// Channel-scoped meeting lifecycle endpoints (distinct from the
// thread-scoped meeting pipeline in meeting.go). Kicked off by a
// "开始会议" button in the channel header; the recording panel lives
// next to the channel messages.
//
//   POST   /api/channels/:channelID/meetings      — start
//   GET    /api/channels/:channelID/meetings      — list (recent N)
//   GET    /api/meetings/:id                      — detail
//   POST   /api/meetings/:id/recording            — set audio_url +
//                                                   trigger transcribe
//   PATCH  /api/meetings/:id/notes                — save live notes
//   PUT    /api/meetings/:id/highlights           — replace highlights
//
// Audio capture + upload happen client-side (MediaRecorder →
// Volcengine TOS). The client PUTs to the bucket and then POSTs the
// resulting URL via /recording — the server never handles raw audio
// bytes, keeping the API path light.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type startChannelMeetingRequest struct {
	Topic string `json:"topic"`
}

type channelMeetingResponse struct {
	ID            string          `json:"id"`
	ChannelID     string          `json:"channel_id"`
	WorkspaceID   string          `json:"workspace_id"`
	StartedBy     string          `json:"started_by"`
	Topic         string          `json:"topic"`
	Status        string          `json:"status"`
	AudioURL      string          `json:"audio_url,omitempty"`
	AudioDuration int32           `json:"audio_duration,omitempty"`
	TaskID        string          `json:"task_id,omitempty"`
	Transcript    json.RawMessage `json:"transcript,omitempty"`
	Summary       json.RawMessage `json:"summary,omitempty"`
	Notes         string          `json:"notes"`
	Highlights    json.RawMessage `json:"highlights"`
	FailureReason string          `json:"failure_reason,omitempty"`
	StartedAt     string          `json:"started_at"`
	EndedAt       string          `json:"ended_at,omitempty"`
	UpdatedAt     string          `json:"updated_at"`
}

func channelMeetingToResponse(m db.Meeting) channelMeetingResponse {
	resp := channelMeetingResponse{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		StartedBy:   uuidToString(m.StartedBy),
		Topic:       m.Topic,
		Status:      m.Status,
		Notes:       m.Notes,
	}
	if m.AudioUrl.Valid {
		resp.AudioURL = m.AudioUrl.String
	}
	if m.AudioDuration.Valid {
		resp.AudioDuration = m.AudioDuration.Int32
	}
	if m.TaskID.Valid {
		resp.TaskID = m.TaskID.String
	}
	if len(m.Transcript) > 0 {
		resp.Transcript = json.RawMessage(m.Transcript)
	}
	if len(m.Summary) > 0 {
		resp.Summary = json.RawMessage(m.Summary)
	}
	if len(m.Highlights) > 0 {
		resp.Highlights = json.RawMessage(m.Highlights)
	} else {
		resp.Highlights = json.RawMessage("[]")
	}
	if m.FailureReason.Valid {
		resp.FailureReason = m.FailureReason.String
	}
	if m.StartedAt.Valid {
		resp.StartedAt = m.StartedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if m.EndedAt.Valid {
		resp.EndedAt = m.EndedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if m.UpdatedAt.Valid {
		resp.UpdatedAt = m.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return resp
}

func channelMeetingID(r *http.Request) string {
	for _, key := range []string{"channelID", "id"} {
		if v := chi.URLParam(r, key); v != "" {
			return v
		}
	}
	return ""
}

// StartChannelMeeting — POST /api/channels/:channelID/meetings
func (h *Handler) StartChannelMeeting(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	channelID := channelMeetingID(r)
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "channel id required")
		return
	}

	var req startChannelMeetingRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	m, err := h.Queries.CreateMeeting(r.Context(), db.CreateMeetingParams{
		ChannelID:   parseUUID(channelID),
		WorkspaceID: parseUUID(workspaceID),
		StartedBy:   parseUUID(userID),
		Topic:       req.Topic,
	})
	if err != nil {
		slog.Error("create channel meeting", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start meeting")
		return
	}

	if h.Hub != nil {
		h.Hub.PushSessionUpdate(workspaceID, map[string]any{
			"type":    "channel_meeting:started",
			"meeting": channelMeetingToResponse(m),
		})
	}
	writeJSON(w, http.StatusCreated, channelMeetingToResponse(m))
}

// ListChannelMeetings — GET /api/channels/:channelID/meetings
func (h *Handler) ListChannelMeetings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	channelID := channelMeetingID(r)
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "channel id required")
		return
	}
	limit := int32(20)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}
	rows, err := h.Queries.ListMeetingsByChannel(r.Context(), db.ListMeetingsByChannelParams{
		ChannelID: parseUUID(channelID),
		Limit:     limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list meetings")
		return
	}
	out := make([]channelMeetingResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, channelMeetingToResponse(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"meetings": out})
}

// GetChannelMeeting — GET /api/meetings/:id
func (h *Handler) GetChannelMeeting(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	m, err := h.Queries.GetMeeting(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}
	writeJSON(w, http.StatusOK, channelMeetingToResponse(m))
}

type finishChannelMeetingRequest struct {
	AudioURL      string `json:"audio_url"`
	AudioDuration int32  `json:"audio_duration"`
}

// SubmitChannelMeetingRecording — POST /api/meetings/:id/recording
// Client uploads audio to object storage, then hands us the public
// URL. We persist it and kick off the Doubao transcription.
func (h *Handler) SubmitChannelMeetingRecording(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req finishChannelMeetingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.AudioURL == "" {
		writeError(w, http.StatusBadRequest, "audio_url is required")
		return
	}
	m, err := h.Queries.UpdateMeetingRecording(r.Context(), db.UpdateMeetingRecordingParams{
		ID:            parseUUID(id),
		AudioUrl:      pgtype.Text{String: req.AudioURL, Valid: true},
		AudioDuration: pgtype.Int4{Int32: req.AudioDuration, Valid: req.AudioDuration > 0},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update recording")
		return
	}
	if h.MeetingTranscriber != nil {
		h.MeetingTranscriber.TranscribeAsync(
			m.ID,
			req.AudioURL,
			m.Topic,
			uuidToString(m.WorkspaceID),
		)
	}
	writeJSON(w, http.StatusOK, channelMeetingToResponse(m))
}

type updateChannelMeetingNotesRequest struct {
	Notes string `json:"notes"`
}

// UpdateChannelMeetingNotes — PATCH /api/meetings/:id/notes
func (h *Handler) UpdateChannelMeetingNotes(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req updateChannelMeetingNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	m, err := h.Queries.UpdateMeetingNotes(r.Context(), db.UpdateMeetingNotesParams{
		ID:    parseUUID(id),
		Notes: req.Notes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save notes")
		return
	}
	writeJSON(w, http.StatusOK, channelMeetingToResponse(m))
}

type updateChannelMeetingHighlightsRequest struct {
	Highlights []map[string]any `json:"highlights"`
}

// UploadChannelMeetingAudio — POST /api/meetings/:id/audio
// Accepts the recorded audio blob (multipart) from the browser,
// uploads it via the S3 storage adapter, then kicks off Doubao
// transcription. 100 MiB cap matches the handler's default
// maxAudioUploadBytes.
const channelMeetingAudioCap = 100 << 20

func (h *Handler) UploadChannelMeetingAudio(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "object storage not configured")
		return
	}
	id := chi.URLParam(r, "id")
	m, err := h.Queries.GetMeeting(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, channelMeetingAudioCap)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read audio: "+err.Error())
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/webm"
	}
	filename := header.Filename
	if filename == "" {
		filename = fmt.Sprintf("meeting-%s.webm", id)
	}
	key := fmt.Sprintf("meetings/%s/%d-%s",
		uuidToString(m.ID),
		time.Now().Unix(),
		filename,
	)

	url, err := h.Storage.Upload(r.Context(), key, data, contentType, filename)
	if err != nil {
		slog.Error("meeting audio upload", "error", err)
		writeError(w, http.StatusInternalServerError, "upload failed: "+err.Error())
		return
	}

	durationSec := int32(0)
	if v := r.FormValue("duration"); v != "" {
		if n, pErr := strconv.Atoi(v); pErr == nil && n > 0 {
			durationSec = int32(n)
		}
	}

	m, err = h.Queries.UpdateMeetingRecording(r.Context(), db.UpdateMeetingRecordingParams{
		ID:            parseUUID(id),
		AudioUrl:      pgtype.Text{String: url, Valid: true},
		AudioDuration: pgtype.Int4{Int32: durationSec, Valid: durationSec > 0},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "persist recording failed")
		return
	}
	if h.MeetingTranscriber != nil {
		h.MeetingTranscriber.TranscribeAsync(
			m.ID, url, m.Topic, uuidToString(m.WorkspaceID),
		)
	}
	writeJSON(w, http.StatusOK, channelMeetingToResponse(m))
}

// UpdateChannelMeetingHighlights — PUT /api/meetings/:id/highlights
// Highlights are arbitrary marker objects — `{t: <seconds>, text:
// "..."}` is the minimal shape but the server doesn't enforce a
// schema so the UI can extend it freely.
func (h *Handler) UpdateChannelMeetingHighlights(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req updateChannelMeetingHighlightsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	raw, _ := json.Marshal(req.Highlights)
	m, err := h.Queries.UpdateMeetingHighlights(r.Context(), db.UpdateMeetingHighlightsParams{
		ID:         parseUUID(id),
		Highlights: raw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save highlights")
		return
	}
	writeJSON(w, http.StatusOK, channelMeetingToResponse(m))
}

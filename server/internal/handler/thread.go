package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Thread represents a message thread within a channel.
// This mirrors the `thread` table defined in docs/data-model.md.
// TODO: remove this struct once sqlc generates db.Thread from the migration.
type Thread struct {
	ID          pgtype.UUID        `json:"id"`
	ChannelID   pgtype.UUID        `json:"channel_id"`
	Title       pgtype.Text        `json:"title"`
	ReplyCount  int32              `json:"reply_count"`
	LastReplyAt pgtype.Timestamptz `json:"last_reply_at"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
}

// ListThreads - GET /api/channels/{channelID}/threads
// Returns threads for a channel, ordered by last_reply_at DESC.
func (h *Handler) ListThreads(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	// TODO: wire after sqlc generation and thread table migration.
	// Expected query: SELECT * FROM thread WHERE channel_id = $1 ORDER BY last_reply_at DESC LIMIT $2 OFFSET $3
	// For now, use a raw query via the DB executor.
	threads, err := h.listThreadsByChannel(r.Context(), channelID, int32(limit), int32(offset))
	if err != nil {
		slog.Warn("list threads failed", "error", err, "channel_id", channelID)
		writeError(w, http.StatusInternalServerError, "failed to list threads")
		return
	}

	resp := make([]map[string]any, len(threads))
	for i, t := range threads {
		resp[i] = threadToResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": resp})
}

// GetThread - GET /api/threads/{threadID}
// Returns thread with metadata (reply_count, last_reply_at, title).
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	// TODO: wire after sqlc generation and thread table migration.
	thread, err := h.getThread(r.Context(), threadID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "thread not found")
			return
		}
		slog.Warn("get thread failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to get thread")
		return
	}

	writeJSON(w, http.StatusOK, threadToResponse(thread))
}

func threadToResponse(t Thread) map[string]any {
	return map[string]any{
		"id":            uuidToString(t.ID),
		"channel_id":    uuidToString(t.ChannelID),
		"title":         textToPtr(t.Title),
		"reply_count":   t.ReplyCount,
		"last_reply_at": timestampToPtr(t.LastReplyAt),
		"created_at":    timestampToString(t.CreatedAt),
	}
}

// listThreadsByChannel queries threads for a channel.
// TODO: replace with h.Queries.ListThreadsByChannel after sqlc generation.
func (h *Handler) listThreadsByChannel(ctx context.Context, channelID string, limit, offset int32) ([]Thread, error) {
	if h.DB == nil {
		return nil, nil
	}

	rows, err := h.DB.Query(ctx,
		`SELECT id, channel_id, title, reply_count, last_reply_at, created_at
		 FROM thread WHERE channel_id = $1 ORDER BY last_reply_at DESC NULLS LAST LIMIT $2 OFFSET $3`,
		parseUUID(channelID), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.ChannelID, &t.Title, &t.ReplyCount, &t.LastReplyAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// getThread fetches a single thread by ID.
// TODO: replace with h.Queries.GetThread after sqlc generation.
func (h *Handler) getThread(ctx context.Context, threadID string) (Thread, error) {
	if h.DB == nil {
		return Thread{}, pgx.ErrNoRows
	}

	var t Thread
	err := h.DB.QueryRow(ctx,
		`SELECT id, channel_id, title, reply_count, last_reply_at, created_at
		 FROM thread WHERE id = $1`,
		parseUUID(threadID),
	).Scan(&t.ID, &t.ChannelID, &t.Title, &t.ReplyCount, &t.LastReplyAt, &t.CreatedAt)
	return t, err
}

// upsertThread creates or updates a thread for a parent message.
// TODO: replace with h.Queries.UpsertThread after sqlc generation.
func (h *Handler) upsertThread(ctx context.Context, threadID, channelID pgtype.UUID) error {
	if h.DB == nil {
		return nil
	}

	_, err := h.DB.Exec(ctx,
		`INSERT INTO thread (id, channel_id, reply_count, created_at)
		 VALUES ($1, $2, 0, now())
		 ON CONFLICT (id) DO NOTHING`,
		threadID, channelID,
	)
	return err
}

// incrementThreadReplyCount increments the reply_count and updates last_reply_at.
// TODO: replace with h.Queries.IncrementThreadReplyCount after sqlc generation.
func (h *Handler) incrementThreadReplyCount(ctx context.Context, threadID pgtype.UUID) {
	if h.DB == nil {
		return
	}

	_, err := h.DB.Exec(ctx,
		`UPDATE thread SET reply_count = reply_count + 1, last_reply_at = now() WHERE id = $1`,
		threadID,
	)
	if err != nil {
		slog.Warn("increment thread reply count failed", "thread_id", uuidToString(threadID), "error", err)
	}
}

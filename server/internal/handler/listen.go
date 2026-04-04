package handler

import (
	"net/http"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GET /api/listen?agent_id=X&channel_id=X&session_id=X&after_id=X&timeout=30
// Long-polls for new messages, returns when a message arrives or timeout.
func (h *Handler) Listen(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	channelID := r.URL.Query().Get("channel_id")
	sessionID := r.URL.Query().Get("session_id")
	afterID := r.URL.Query().Get("after_id")
	timeoutSec := queryInt(r, "timeout", 30)

	if agentID == "" && channelID == "" && sessionID == "" {
		writeError(w, http.StatusBadRequest, "specify agent_id, channel_id, or session_id")
		return
	}

	if timeoutSec > 120 {
		timeoutSec = 120
	}

	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	pollInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		var messages []map[string]any

		if channelID != "" {
			msgs, err := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
				ChannelID: parseUUID(channelID),
				Limit:     10,
				Offset:    0,
			})
			if err == nil {
				for _, m := range msgs {
					if afterID != "" && uuidToString(m.ID) <= afterID {
						continue
					}
					messages = append(messages, messageToResponse(m))
				}
			}
		} else if sessionID != "" {
			msgs, err := h.Queries.ListSessionMessages(r.Context(), db.ListSessionMessagesParams{
				SessionID: parseUUID(sessionID),
				Limit:     10,
				Offset:    0,
			})
			if err == nil {
				for _, m := range msgs {
					if afterID != "" && uuidToString(m.ID) <= afterID {
						continue
					}
					messages = append(messages, messageToResponse(m))
				}
			}
		} else if agentID != "" {
			msgs, err := h.Queries.ListDMMessages(r.Context(), db.ListDMMessagesParams{
				WorkspaceID:   parseUUID(resolveWorkspaceID(r)),
				SenderID:      parseUUID(agentID),
				SenderType:    "member",
				RecipientID:   parseUUID(agentID),
				RecipientType: strToText("agent"),
				Limit:         10,
				Offset:        0,
			})
			if err == nil {
				for _, m := range msgs {
					if afterID != "" && uuidToString(m.ID) <= afterID {
						continue
					}
					messages = append(messages, messageToResponse(m))
				}
			}
		}

		if len(messages) > 0 {
			var history []map[string]any
			if channelID != "" {
				hist, _ := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
					ChannelID: parseUUID(channelID),
					Limit:     20,
					Offset:    0,
				})
				for _, m := range hist {
					history = append(history, messageToResponse(m))
				}
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"messages":    messages,
				"history":     history,
				"has_new":     true,
				"poll_status": "message_received",
			})
			return
		}

		time.Sleep(pollInterval)
	}

	// Timeout — no messages
	writeJSON(w, http.StatusOK, map[string]any{
		"messages":    []any{},
		"has_new":     false,
		"poll_status": "timeout",
	})
}

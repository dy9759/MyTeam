// Package handler: conversation.go — per-user archive state for DMs.
//
// Unlike channels (archived workspace-wide), DMs archive on a per-user
// basis because each participant manages their own inbox. Backed by the
// dm_conversation_state table.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// POST /api/conversations/archive
// Body: {"peer_id": "uuid", "peer_type": "member"|"agent", "archived": bool}
func (h *Handler) ArchiveDMConversation(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		PeerID   string `json:"peer_id"`
		PeerType string `json:"peer_type"`
		Archived bool   `json:"archived"`
	}
	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PeerID == "" {
		writeError(w, http.StatusBadRequest, "peer_id required")
		return
	}
	if req.PeerType != "member" && req.PeerType != "agent" {
		writeError(w, http.StatusBadRequest, "peer_type must be 'member' or 'agent'")
		return
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

	var archivedAt pgtype.Timestamptz
	if req.Archived {
		archivedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}

	err := h.Queries.UpsertDMArchiveState(r.Context(), db.UpsertDMArchiveStateParams{
		UserID:      parseUUID(userID),
		PeerID:      parseUUID(req.PeerID),
		PeerType:    req.PeerType,
		WorkspaceID: parseUUID(workspaceID),
		ArchivedAt:  archivedAt,
	})
	if err != nil {
		slog.Warn("archive dm conversation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update archive state")
		return
	}

	event := "conversation:archived"
	if !req.Archived {
		event = "conversation:unarchived"
	}
	h.publish(event, workspaceID, "member", userID, map[string]any{
		"peer_id":   req.PeerID,
		"peer_type": req.PeerType,
	})

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/conversations/archived
// Returns the list of (peer_id, peer_type) pairs the current user has
// archived in this workspace. Frontend uses this to split conversations
// into active/archived buckets without a round-trip per peer.
func (h *Handler) ListArchivedDMPeers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := h.Queries.ListDMArchivedPeers(r.Context(), db.ListDMArchivedPeersParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Warn("list archived dm peers failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list archived conversations")
		return
	}

	archived := make([]map[string]any, len(rows))
	for i, r := range rows {
		archived[i] = map[string]any{
			"peer_id":   uuidToString(r.PeerID),
			"peer_type": r.PeerType,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

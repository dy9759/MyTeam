// Workspace secret CRUD handlers (PRD §11).
//
// Secrets are AES-256-GCM encrypted at rest. The master key is loaded from
// the MYTEAM_SECRET_KEY env var (base64-encoded 32 bytes). Per crypto.go,
// each secret binds AAD = "<workspace_id>/<key>" so an admin cannot move a
// ciphertext blob from one row to another. Plain values are returned only
// on the per-key GET endpoint and are NEVER logged or surfaced via the LIST
// endpoint.
//
// All endpoints require workspace admin (or owner). Member-tier reads are
// forbidden because secret values can authorize external integrations.
package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MyAIOSHub/MyTeam/server/internal/auth"
	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/pkg/crypto"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

const secretKeyEnvVar = "MYTEAM_SECRET_KEY"

// loadSecretMasterKey reads MYTEAM_SECRET_KEY (base64) and returns the
// 32-byte AES key. Returns an explanatory error when the env var is unset
// or malformed so the handler can surface a 503.
func loadSecretMasterKey() ([]byte, error) {
	raw := os.Getenv(secretKeyEnvVar)
	if raw == "" {
		return nil, fmt.Errorf("%s not set", secretKeyEnvVar)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", secretKeyEnvVar, err)
	}
	if len(decoded) != crypto.KeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", secretKeyEnvVar, crypto.KeySize, len(decoded))
	}
	return decoded, nil
}

// secretAAD builds the additional-authenticated-data string used by every
// encrypt/decrypt for a given (workspace, key). Binding the AAD prevents
// ciphertext transplantation between rows.
func secretAAD(workspaceID, key string) []byte {
	return []byte(workspaceID + "/" + key)
}

type workspaceSecretListItem struct {
	Key       string     `json:"key"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	RotatedAt *time.Time `json:"rotated_at,omitempty"`
}

// requireWorkspaceAdmin centralizes the admin/owner check for all secret
// endpoints. Returns false (and writes the appropriate errcode response)
// when the caller is not authorized.
func (h *Handler) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	userID := requestUserID(r)
	if userID == "" {
		errcode.Write(w, errcode.AuthUnauthorized, "", nil)
		return false
	}
	wsUUID, err := uuid.Parse(workspaceID)
	if err != nil {
		errcode.Write(w, errcode.AuthForbidden, "invalid workspace id", nil)
		return false
	}
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		errcode.Write(w, errcode.AuthForbidden, "invalid user id", nil)
		return false
	}
	if err := h.Guards.RequireAdminOrAbove(r.Context(), wsUUID, userUUID); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			errcode.Write(w, errcode.AuthForbidden, "", nil)
			return false
		}
		// Other errors (e.g. no member row) — the user is not authorized
		// for this workspace; surface as forbidden rather than leaking
		// internal lookup details.
		errcode.Write(w, errcode.AuthForbidden, "", nil)
		return false
	}
	return true
}

// ListWorkspaceSecrets returns metadata for every secret in the workspace.
// The encrypted value is intentionally omitted; admins must request a
// specific key to read its plaintext.
func (h *Handler) ListWorkspaceSecrets(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}

	rows, err := h.Queries.ListWorkspaceSecretKeys(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("workspace_secret list failed", "workspace_id", workspaceID, "err", err)
		writeError(w, http.StatusInternalServerError, "list secrets failed")
		return
	}

	out := make([]workspaceSecretListItem, 0, len(rows))
	for _, row := range rows {
		item := workspaceSecretListItem{
			Key:       row.Key,
			CreatedBy: uuidToString(row.CreatedBy),
			CreatedAt: row.CreatedAt.Time,
		}
		if row.RotatedAt.Valid {
			t := row.RotatedAt.Time
			item.RotatedAt = &t
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetWorkspaceSecret returns the decrypted value for a single secret. Only
// admins/owners can call this; the plaintext is the load-bearing material
// (e.g. an external API key) and must not appear in any list response.
func (h *Handler) GetWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	key := chi.URLParam(r, "key")
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	masterKey, err := loadSecretMasterKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "secret encryption not configured: "+err.Error())
		return
	}

	row, err := h.Queries.GetWorkspaceSecret(r.Context(), db.GetWorkspaceSecretParams{
		WorkspaceID: parseUUID(workspaceID),
		Key:         key,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		slog.Error("workspace_secret lookup failed", "workspace_id", workspaceID, "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	plaintext, err := crypto.Decrypt(row.ValueEncrypted, masterKey, secretAAD(workspaceID, key))
	if err != nil {
		// Do not log the ciphertext itself; only metadata. AAD mismatch
		// (e.g. ciphertext from another row) and tag-failure both surface
		// here.
		slog.Error("workspace_secret decrypt failed", "workspace_id", workspaceID, "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "decrypt failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"key":   key,
		"value": string(plaintext),
	})
}

type setWorkspaceSecretRequest struct {
	Value string `json:"value"`
}

// SetWorkspaceSecret upserts a secret. The CreateWorkspaceSecret query
// performs ON CONFLICT DO UPDATE which doubles as the rotation path —
// rotated_at advances on each subsequent write.
func (h *Handler) SetWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	key := chi.URLParam(r, "key")
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	masterKey, err := loadSecretMasterKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "secret encryption not configured: "+err.Error())
		return
	}

	var req setWorkspaceSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value required")
		return
	}

	encrypted, err := crypto.Encrypt([]byte(req.Value), masterKey, secretAAD(workspaceID, key))
	if err != nil {
		slog.Error("workspace_secret encrypt failed", "workspace_id", workspaceID, "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "encrypt failed")
		return
	}

	userID := requestUserID(r)
	if _, err := h.Queries.CreateWorkspaceSecret(r.Context(), db.CreateWorkspaceSecretParams{
		WorkspaceID:    parseUUID(workspaceID),
		Key:            key,
		ValueEncrypted: encrypted,
		CreatedBy:      parseUUID(userID),
	}); err != nil {
		slog.Error("workspace_secret save failed", "workspace_id", workspaceID, "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "status": "ok"})
}

// SetStorageSecrets writes the 5 Volcengine TOS keys atomically.
// Convenience wrapper over SetWorkspaceSecret so the UI doesn't have
// to fire 5 sequential PUTs. Empty values are skipped (no-op for that
// key) so callers can rotate one field at a time.
//
// PUT /api/workspaces/{id}/secrets/storage
//
//	{ "tos_access_key_id": "...", "tos_secret_access_key": "...",
//	  "tos_bucket": "...", "tos_region": "cn-beijing",
//	  "tos_endpoint": "https://tos-s3-cn-beijing.volces.com" }
func (h *Handler) SetStorageSecrets(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}
	masterKey, err := loadSecretMasterKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "secret encryption not configured: "+err.Error())
		return
	}

	var req struct {
		TOSAccessKeyID     string `json:"tos_access_key_id"`
		TOSSecretAccessKey string `json:"tos_secret_access_key"`
		TOSBucket          string `json:"tos_bucket"`
		TOSRegion          string `json:"tos_region"`
		TOSEndpoint        string `json:"tos_endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	pairs := []struct{ key, value string }{
		{"tos_access_key_id", req.TOSAccessKeyID},
		{"tos_secret_access_key", req.TOSSecretAccessKey},
		{"tos_bucket", req.TOSBucket},
		{"tos_region", req.TOSRegion},
		{"tos_endpoint", req.TOSEndpoint},
	}
	userID := requestUserID(r)
	written := 0
	for _, p := range pairs {
		if p.value == "" {
			continue
		}
		encrypted, encErr := crypto.Encrypt([]byte(p.value), masterKey, secretAAD(workspaceID, p.key))
		if encErr != nil {
			slog.Error("storage secret encrypt failed", "workspace_id", workspaceID, "key", p.key, "err", encErr)
			writeError(w, http.StatusInternalServerError, "encrypt failed for "+p.key)
			return
		}
		if _, dbErr := h.Queries.CreateWorkspaceSecret(r.Context(), db.CreateWorkspaceSecretParams{
			WorkspaceID:    parseUUID(workspaceID),
			Key:            p.key,
			ValueEncrypted: encrypted,
			CreatedBy:      parseUUID(userID),
		}); dbErr != nil {
			slog.Error("storage secret save failed", "workspace_id", workspaceID, "key", p.key, "err", dbErr)
			writeError(w, http.StatusInternalServerError, "save failed for "+p.key)
			return
		}
		written++
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "written": written})
}

// DeleteWorkspaceSecret removes a secret outright. No 404 is returned when
// the key is absent because the operation is idempotent and the underlying
// query is :exec.
func (h *Handler) DeleteWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	key := chi.URLParam(r, "key")
	if !h.requireWorkspaceAdmin(w, r, workspaceID) {
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := h.Queries.DeleteWorkspaceSecret(r.Context(), db.DeleteWorkspaceSecretParams{
		WorkspaceID: parseUUID(workspaceID),
		Key:         key,
	}); err != nil {
		slog.Error("workspace_secret delete failed", "workspace_id", workspaceID, "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

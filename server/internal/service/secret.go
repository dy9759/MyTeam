package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/pkg/crypto"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

const secretKeyEnvVar = "MYTEAM_SECRET_KEY"

// SecretService reads and decrypts workspace-scoped secrets.
type SecretService struct {
	Q *db.Queries
}

func NewSecretService(q *db.Queries) *SecretService {
	return &SecretService{Q: q}
}

// GetPlaintext returns a decrypted workspace secret value.
func (s *SecretService) GetPlaintext(ctx context.Context, workspaceID uuid.UUID, key string) (string, error) {
	if s.Q == nil {
		return "", fmt.Errorf("secret service: queries required")
	}
	if key == "" {
		return "", fmt.Errorf("secret service: key required")
	}

	masterKey, err := loadSecretMasterKey()
	if err != nil {
		return "", err
	}

	row, err := s.Q.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: pgUUID(workspaceID),
		Key:         key,
	})
	if err != nil {
		return "", err
	}

	plaintext, err := crypto.Decrypt(row.ValueEncrypted, masterKey, secretAAD(workspaceID.String(), key))
	if err != nil {
		return "", fmt.Errorf("decrypt workspace secret %q: %w", key, err)
	}
	return string(plaintext), nil
}

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

func secretAAD(workspaceID, key string) []byte {
	return []byte(workspaceID + "/" + key)
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

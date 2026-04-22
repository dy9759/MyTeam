package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// sqlcMember adapts *db.Queries to MemberLookup.
type sqlcMember struct{ q *db.Queries }

func (s sqlcMember) GetMemberRole(ctx context.Context, ws, user uuid.UUID) (string, error) {
	row, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      pgUUID(user),
		WorkspaceID: pgUUID(ws),
	})
	if err != nil {
		return "", err
	}
	return row.Role, nil
}

// sqlcAgent adapts *db.Queries to AgentLookup.
type sqlcAgent struct{ q *db.Queries }

func (s sqlcAgent) GetAgentOwnerID(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	row, err := s.q.GetAgent(ctx, pgUUID(id))
	if err != nil {
		return uuid.Nil, err
	}
	if !row.OwnerID.Valid {
		return uuid.Nil, errors.New("agent has no owner")
	}
	return uuid.UUID(row.OwnerID.Bytes), nil
}

// NewGuards builds Guards backed by sqlc.
func NewGuards(q *db.Queries) Guards {
	return Guards{Member: sqlcMember{q: q}, Agent: sqlcAgent{q: q}}
}

// pgUUID converts a google/uuid.UUID to the pgtype representation used by sqlc.
func pgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

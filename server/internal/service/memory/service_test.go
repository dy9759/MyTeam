package memory

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTestQ(t *testing.T) *db.Queries {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return db.New(pool)
}

// seedFile creates a workspace + user + file_index row to act as a
// valid RawRef target. Returns workspaceID + fileIndexID.
func seedFile(t *testing.T, q *db.Queries) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	ws, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "Mem Test " + t.Name(),
		Slug:        "mem-" + uuid.NewString()[:8],
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: "MEM",
	})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Name:  "MemUser " + t.Name(),
		Email: "memuser+" + uuid.NewString()[:8] + "@example.com",
	})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	fi, err := q.CreateFileIndex(ctx, db.CreateFileIndexParams{
		WorkspaceID:          ws.ID,
		UploaderIdentityID:   user.ID,
		UploaderIdentityType: "member",
		OwnerID:              user.ID,
		SourceType:           "channel",
		SourceID:             ws.ID, // any uuid; not FK-checked here
		FileName:             "raw.bin",
		FileSize:             pgtype.Int8{Int64: 1, Valid: true},
		StoragePath:          pgtype.Text{String: "/tmp/raw.bin", Valid: true},
		AccessScope:          []byte(`{"scope":"channel"}`),
	})
	if err != nil {
		t.Fatalf("file_index: %v", err)
	}
	return uuid.UUID(ws.ID.Bytes), uuid.UUID(user.ID.Bytes), uuid.UUID(fi.ID.Bytes)
}

func TestMemoryService_AppendValidatesRaw(t *testing.T) {
	q := newTestQ(t)
	svc := NewService(q)
	wsID, userID, _ := seedFile(t, q)

	// Bad raw id → ErrInvalidRaw.
	_, err := svc.Append(context.Background(), AppendInput{
		WorkspaceID: wsID,
		Type:        TypeFact,
		Scope:       ScopeSharedSummary,
		Source:      "test",
		Raw:         RawRef{Kind: RawFileIndex, ID: uuid.New()}, // not in DB
		Summary:     "x",
		CreatedBy:   userID,
	})
	if !errors.Is(err, ErrInvalidRaw) {
		t.Fatalf("expected ErrInvalidRaw, got %v", err)
	}
}

func TestMemoryService_HappyPath(t *testing.T) {
	q := newTestQ(t)
	ctx := context.Background()
	svc := NewService(q)
	wsID, userID, fileID := seedFile(t, q)

	// 1. Append candidate.
	m, err := svc.Append(ctx, AppendInput{
		WorkspaceID: wsID,
		Type:        TypeSummary,
		Scope:       ScopeSharedSummary,
		Source:      "meeting",
		Raw:         RawRef{Kind: RawFileIndex, ID: fileID},
		Summary:     "Q3 review",
		Body:        "ship phase 1",
		Tags:        []string{"q3", "review"},
		Entities:    []string{"alice", "bob"},
		Confidence:  0.7,
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if m.Status != StatusCandidate {
		t.Fatalf("default status: want candidate, got %s", m.Status)
	}
	if m.Version != 1 {
		t.Fatalf("version: want 1, got %d", m.Version)
	}

	// 2. Promote to confirmed → version bumps.
	m2, err := svc.Promote(ctx, m.ID)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if m2.Status != StatusConfirmed {
		t.Fatalf("status: want confirmed, got %s", m2.Status)
	}
	if m2.Version != 2 {
		t.Fatalf("version: want 2, got %d", m2.Version)
	}

	// 3. ListByWorkspace filtered by scope.
	list, err := svc.ListByWorkspace(ctx, wsID, ListFilter{
		Scope: ScopeSharedSummary,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 1 {
		t.Fatalf("list empty")
	}

	// 4. GetByRaw returns the row.
	by, err := svc.GetByRaw(ctx, RawRef{Kind: RawFileIndex, ID: fileID})
	if err != nil {
		t.Fatalf("GetByRaw: %v", err)
	}
	if len(by) != 1 || by[0].ID != m.ID {
		t.Fatalf("GetByRaw mismatch: %#v", by)
	}

	// 5. Tags + entities preserved.
	got, err := svc.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got.Tags, ",") != "q3,review" {
		t.Errorf("tags: %v", got.Tags)
	}

	// 6. Archive → status archived.
	m3, err := svc.Archive(ctx, m.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if m3.Status != StatusArchived {
		t.Errorf("status after archive: %s", m3.Status)
	}
}

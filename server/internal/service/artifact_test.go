// artifact_test.go — DB-backed tests for ArtifactService. Requires
// DATABASE_URL pointing at a migrated myteam DB (migration 058+
// for the artifact/review tables).
package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// pgxToUUID converts a pgtype.UUID into a google/uuid.UUID for use as the
// service-layer request input shape. Mirrors what handler/HTTP layer would do.
func pgxToUUID(t *testing.T, p pgtype.UUID) uuid.UUID {
	t.Helper()
	if !p.Valid {
		t.Fatalf("pgxToUUID: pgtype.UUID not valid")
	}
	return uuid.UUID(p.Bytes)
}

// artifactTestEnv bundles the IDs needed to insert artifacts: a workspace,
// member, plan, project_run (for the run_id FK), and a task (for the task_id
// FK + version scoping). All FKs satisfied; cleanup happens via test DB
// reset / suffixed names.
type artifactTestEnv struct {
	WorkspaceID pgtype.UUID
	MemberID    pgtype.UUID
	PlanID      pgtype.UUID
	ProjectID   pgtype.UUID
	RunID       pgtype.UUID
	TaskID      pgtype.UUID
}

func setupArtifactEnv(t *testing.T, q *db.Queries) artifactTestEnv {
	t.Helper()
	ctx := context.Background()

	wsID := createTestWorkspace(t, q)
	memberID := createTestUser(t, q, "artifact+"+t.Name()+"@example.com", "Artifact Tester")

	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID: wsID,
		Title:       "Plan for " + t.Name(),
		Description: pgtype.Text{String: "test plan", Valid: true},
		SourceType:  pgtype.Text{},
		SourceRefID: pgtype.UUID{},
		Constraints: pgtype.Text{},
		ExpectedOutput: pgtype.Text{
			String: "artifacts",
			Valid:  true,
		},
		CreatedBy: memberID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// project.created_by is NOT NULL (migration 039) but the sqlc CreateProject
	// query (migration 040 era) does not set it. Insert via raw SQL so the
	// project_run FK is satisfied.
	pool := openTestPool(t)
	var projectID pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'running', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, wsID, "Project for "+t.Name(), memberID).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project (raw): %v", err)
	}

	run, err := q.CreateProjectRun(ctx, db.CreateProjectRunParams{
		PlanID:    plan.ID,
		ProjectID: projectID,
		Status:    "running",
	})
	if err != nil {
		t.Fatalf("create project_run: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:             plan.ID,
		RunID:              run.ID,
		WorkspaceID:        wsID,
		Title:              "Task for " + t.Name(),
		Description:        pgtype.Text{String: "do work", Valid: true},
		StepOrder:          pgtype.Int4{Int32: 0, Valid: true},
		DependsOn:          nil,
		PrimaryAssigneeID:  pgtype.UUID{},
		FallbackAgentIds:   nil,
		RequiredSkills:     nil,
		CollaborationMode:  pgtype.Text{},
		AcceptanceCriteria: pgtype.Text{},
		TimeoutRule:        nil,
		RetryRule:          nil,
		EscalationPolicy:   nil,
		InputContextRefs:   nil,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	return artifactTestEnv{
		WorkspaceID: wsID,
		MemberID:    memberID,
		PlanID:      plan.ID,
		ProjectID:   projectID,
		RunID:       run.ID,
		TaskID:      task.ID,
	}
}

// openTestPool opens a fresh pool to DATABASE_URL for raw inserts that the
// sqlc-generated *db.Queries can't perform (e.g. tables whose CREATE columns
// aren't covered by an existing sqlc query). Closed via t.Cleanup.
func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCreateHeadless_Success(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)

	a, err := svc.CreateHeadless(context.Background(), CreateHeadlessRequest{
		TaskID:        pgxToUUID(t, env.TaskID),
		RunID:         pgxToUUID(t, env.RunID),
		ArtifactType:  ArtifactTypeReport,
		Title:         "Test report v1",
		Summary:       "synthesized summary",
		Content:       map[string]any{"sections": []string{"intro", "body"}},
		CreatedByID:   pgxToUUID(t, env.MemberID),
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("CreateHeadless: %v", err)
	}
	if a == nil || !a.ID.Valid {
		t.Fatal("expected returned artifact with valid ID")
	}
	if a.Version != 1 {
		t.Fatalf("expected version=1, got %d", a.Version)
	}
	if a.ArtifactType != ArtifactTypeReport {
		t.Fatalf("expected artifact_type=%s, got %s", ArtifactTypeReport, a.ArtifactType)
	}
	if a.RetentionClass != RetentionPermanent {
		t.Fatalf("expected default retention=%s, got %s", RetentionPermanent, a.RetentionClass)
	}
	if a.FileIndexID.Valid {
		t.Fatal("headless artifact should have NULL file_index_id")
	}
	// Content is JSONB; verify round-trip.
	var got map[string]any
	if err := json.Unmarshal(a.Content, &got); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if _, ok := got["sections"]; !ok {
		t.Fatalf("content missing expected key: %#v", got)
	}
}

func TestCreateHeadless_RejectsNilContent(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)

	_, err := svc.CreateHeadless(context.Background(), CreateHeadlessRequest{
		TaskID:  pgxToUUID(t, env.TaskID),
		RunID:   pgxToUUID(t, env.RunID),
		Content: nil,
	})
	if err == nil {
		t.Fatal("expected error when content is nil")
	}
	if !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("expected ErrArtifactInvalid, got %v", err)
	}
}

func TestCreateHeadless_AutoIncrementsVersion(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)
	ctx := context.Background()

	a1, err := svc.CreateHeadless(ctx, CreateHeadlessRequest{
		TaskID:  pgxToUUID(t, env.TaskID),
		RunID:   pgxToUUID(t, env.RunID),
		Content: map[string]any{"step": 1},
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if a1.Version != 1 {
		t.Fatalf("expected first version=1, got %d", a1.Version)
	}

	a2, err := svc.CreateHeadless(ctx, CreateHeadlessRequest{
		TaskID:  pgxToUUID(t, env.TaskID),
		RunID:   pgxToUUID(t, env.RunID),
		Content: map[string]any{"step": 2},
	})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if a2.Version != 2 {
		t.Fatalf("expected second version=2, got %d", a2.Version)
	}
}

func TestCreateWithFile_Success(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)
	ctx := context.Background()

	// Insert a file_index row scoped to the project (uploader is the member).
	scopeJSON, _ := json.Marshal(map[string]any{"scope": "project"})
	fi, err := q.CreateFileIndex(ctx, db.CreateFileIndexParams{
		WorkspaceID:          env.WorkspaceID,
		UploaderIdentityID:   env.MemberID,
		UploaderIdentityType: "member",
		OwnerID:              env.MemberID,
		SourceType:           "project",
		SourceID:             env.ProjectID,
		FileName:             "design.pdf",
		FileSize:             pgtype.Int8{Int64: 1024, Valid: true},
		ContentType:          pgtype.Text{String: "application/pdf", Valid: true},
		StoragePath:          pgtype.Text{String: "/tmp/design.pdf", Valid: true},
		AccessScope:          scopeJSON,
		ChannelID:            pgtype.UUID{},
		ProjectID:            env.ProjectID,
	})
	if err != nil {
		t.Fatalf("create file_index: %v", err)
	}

	// Create a snapshot for the same file.
	snap, err := q.CreateFileSnapshot(ctx, db.CreateFileSnapshotParams{
		FileID:       fi.ID,
		StoragePath:  "/tmp/design-v1.pdf",
		ReferencedBy: []byte("[]"),
	})
	if err != nil {
		t.Fatalf("create file_snapshot: %v", err)
	}

	a, err := svc.CreateWithFile(ctx, CreateWithFileRequest{
		TaskID:         pgxToUUID(t, env.TaskID),
		RunID:          pgxToUUID(t, env.RunID),
		ArtifactType:   ArtifactTypeDesign,
		Title:          "Design doc v1",
		Summary:        "initial wireframes",
		FileIndexID:    pgxToUUID(t, fi.ID),
		FileSnapshotID: pgxToUUID(t, snap.ID),
		CreatedByID:    pgxToUUID(t, env.MemberID),
		CreatedByType:  "member",
	})
	if err != nil {
		t.Fatalf("CreateWithFile: %v", err)
	}
	if a == nil || !a.ID.Valid {
		t.Fatal("expected returned artifact with valid ID")
	}
	if a.Version != 1 {
		t.Fatalf("expected version=1, got %d", a.Version)
	}
	if !a.FileIndexID.Valid || a.FileIndexID.Bytes != fi.ID.Bytes {
		t.Fatal("expected file_index_id to match")
	}
	if !a.FileSnapshotID.Valid || a.FileSnapshotID.Bytes != snap.ID.Bytes {
		t.Fatal("expected file_snapshot_id to match")
	}
	if a.ArtifactType != ArtifactTypeDesign {
		t.Fatalf("expected artifact_type=%s, got %s", ArtifactTypeDesign, a.ArtifactType)
	}
}

func TestGetWithReviews_ReturnsBoth(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)
	ctx := context.Background()

	a, err := svc.CreateHeadless(ctx, CreateHeadlessRequest{
		TaskID:  pgxToUUID(t, env.TaskID),
		RunID:   pgxToUUID(t, env.RunID),
		Content: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("CreateHeadless: %v", err)
	}

	// Add two reviews so we can verify ordering + count.
	for _, dec := range []string{"request_changes", "approve"} {
		_, err := q.CreateReview(ctx, db.CreateReviewParams{
			TaskID:       env.TaskID,
			ArtifactID:   a.ID,
			SlotID:       pgtype.UUID{},
			ReviewerID:   env.MemberID,
			ReviewerType: pgtype.Text{String: "member", Valid: true},
			Decision:     dec,
			Comment:      pgtype.Text{String: "review-" + dec, Valid: true},
		})
		if err != nil {
			t.Fatalf("create review %s: %v", dec, err)
		}
	}

	bundle, err := svc.GetWithReviews(ctx, pgxToUUID(t, a.ID))
	if err != nil {
		t.Fatalf("GetWithReviews: %v", err)
	}
	if bundle.Artifact.ID.Bytes != a.ID.Bytes {
		t.Fatal("returned artifact ID mismatch")
	}
	if len(bundle.Reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(bundle.Reviews))
	}
	// ListReviewsForArtifact orders by created_at DESC; verify the second
	// (newest) decision came back first.
	if bundle.Reviews[0].Decision != "approve" {
		t.Fatalf("expected newest review decision=approve, got %s", bundle.Reviews[0].Decision)
	}
}

func TestNextVersion_StartsAt1(t *testing.T) {
	q := testDB(t)
	env := setupArtifactEnv(t, q)
	svc := NewArtifactService(q)

	v, err := svc.NextVersion(context.Background(), pgxToUUID(t, env.TaskID))
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected next version=1 for fresh task, got %d", v)
	}
}

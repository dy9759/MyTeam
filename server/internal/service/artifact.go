// Package service: artifact.go — ArtifactService manages versioned task
// artifacts per PRD §4.8 + §9.2.
//
// Two creation paths:
//   - CreateHeadless: Artifact whose payload is JSONB-only (no underlying
//     FileIndex/Snapshot). Used for text summaries, structured results,
//     code-patch diffs, etc.
//   - CreateWithFile: Artifact bound to an existing FileIndex (and optionally
//     FileSnapshot). FileService is the authoritative owner of the actual
//     file upload; this service only creates the artifact pointer row.
//
// The artifact table CHECK constraint enforces the headless rule
// (file_index_id IS NOT NULL OR content IS NOT NULL); we additionally
// guard the same invariant in Go to fail fast with a typed error.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ArtifactType* are the canonical artifact_type values matched by the
// table-level CHECK constraint in migration 058.
const (
	ArtifactTypeDocument  = "document"
	ArtifactTypeDesign    = "design"
	ArtifactTypeCodePatch = "code_patch"
	ArtifactTypeReport    = "report"
	ArtifactTypeFile      = "file"
	ArtifactTypePlanDoc   = "plan_doc"
)

// Retention* mirror the retention_class CHECK values.
const (
	RetentionPermanent = "permanent"
	RetentionTTL       = "ttl"
	RetentionTemp      = "temp"
)

// Errors returned by ArtifactService. Wrapped via fmt.Errorf with context.
var (
	ErrArtifactInvalid         = errors.New("artifact: invalid")
	ErrArtifactProjectMismatch = errors.New("artifact: file_index project_id does not match task project")
)

// ArtifactService manages artifact creation + versioning.
type ArtifactService struct {
	Q *db.Queries
}

// NewArtifactService constructs an ArtifactService bound to the given Queries.
func NewArtifactService(q *db.Queries) *ArtifactService {
	return &ArtifactService{Q: q}
}

// CreateHeadlessRequest is for Artifacts that have no associated file —
// the JSONB content field is the authoritative payload (e.g. text summaries,
// structured results, code-patch diffs).
type CreateHeadlessRequest struct {
	TaskID         uuid.UUID
	SlotID         uuid.UUID // optional, pass uuid.Nil for none
	ExecutionID    uuid.UUID // optional
	RunID          uuid.UUID // required
	ArtifactType   string    // one of ArtifactType* consts; defaults to document
	Title          string
	Summary        string
	Content        any    // marshalable to JSONB; required
	RetentionClass string // permanent|ttl|temp; defaults to permanent
	CreatedByID    uuid.UUID
	CreatedByType  string // member|agent
}

// CreateHeadless creates an Artifact with no underlying FileIndex/Snapshot.
// `content` is required (the headless CHECK constraint enforces this).
//
// Versioning is task-scoped: each call advances the per-task version by
// querying NextArtifactVersion before insert. Concurrent callers may race;
// callers that need strict ordering should serialize at a higher layer.
func (s *ArtifactService) CreateHeadless(ctx context.Context, req CreateHeadlessRequest) (*db.Artifact, error) {
	if req.TaskID == uuid.Nil || req.RunID == uuid.Nil {
		return nil, fmt.Errorf("%w: task_id and run_id required", ErrArtifactInvalid)
	}
	if req.Content == nil {
		return nil, fmt.Errorf("%w: content required for headless artifact", ErrArtifactInvalid)
	}
	if req.RetentionClass == "" {
		req.RetentionClass = RetentionPermanent
	}
	if req.ArtifactType == "" {
		req.ArtifactType = ArtifactTypeDocument
	}

	contentJSON, err := json.Marshal(req.Content)
	if err != nil {
		return nil, fmt.Errorf("marshal content: %w", err)
	}

	version, err := s.Q.NextArtifactVersion(ctx, toPgUUID(req.TaskID))
	if err != nil {
		return nil, fmt.Errorf("next version: %w", err)
	}

	a, err := s.Q.CreateArtifact(ctx, db.CreateArtifactParams{
		TaskID:         toPgUUID(req.TaskID),
		SlotID:         toPgNullUUID(req.SlotID),
		ExecutionID:    toPgNullUUID(req.ExecutionID),
		RunID:          toPgUUID(req.RunID),
		ArtifactType:   req.ArtifactType,
		Version:        version,
		Title:          toPgNullText(req.Title),
		Summary:        toPgNullText(req.Summary),
		Content:        contentJSON,
		FileIndexID:    pgtype.UUID{Valid: false},
		FileSnapshotID: pgtype.UUID{Valid: false},
		RetentionClass: pgtype.Text{String: req.RetentionClass, Valid: true},
		CreatedByID:    toPgNullUUID(req.CreatedByID),
		CreatedByType:  toPgNullText(req.CreatedByType),
	})
	if err != nil {
		return nil, fmt.Errorf("insert artifact: %w", err)
	}
	return &a, nil
}

// CreateWithFileRequest creates an Artifact backed by a FileIndex + FileSnapshot.
// The file row must already belong to the caller workspace; `CreateWithFile`
// does not manage uploads or validate the final task→project match.
type CreateWithFileRequest struct {
	TaskID         uuid.UUID
	SlotID         uuid.UUID
	ExecutionID    uuid.UUID
	RunID          uuid.UUID // required
	ArtifactType   string    // one of ArtifactType* consts; defaults to file
	Title          string
	Summary        string
	Content        any       // optional summary/preview JSON; not authoritative
	FileIndexID    uuid.UUID // required
	FileSnapshotID uuid.UUID // optional
	RetentionClass string
	CreatedByID    uuid.UUID
	CreatedByType  string
}

// CreateWithFile creates an Artifact pointing at an existing FileIndex (and
// optionally FileSnapshot). Callers are responsible for having created the
// FileIndex/Snapshot rows via FileService — this service does NOT manage
// file uploads.
//
// The headless CHECK constraint is satisfied trivially because file_index_id
// is non-NULL.
func (s *ArtifactService) CreateWithFile(ctx context.Context, req CreateWithFileRequest) (*db.Artifact, error) {
	if req.TaskID == uuid.Nil || req.RunID == uuid.Nil || req.FileIndexID == uuid.Nil {
		return nil, fmt.Errorf("%w: task_id, run_id, file_index_id required", ErrArtifactInvalid)
	}
	if req.RetentionClass == "" {
		req.RetentionClass = RetentionPermanent
	}
	if req.ArtifactType == "" {
		req.ArtifactType = ArtifactTypeFile
	}

	// TODO(plan5): validate file_index.project_id against the artifact's
	// resolved project (task→plan→project). UploadArtifact already checks the
	// file belongs to the caller workspace before reaching this service, but
	// CreateWithFile still trusts the caller for the final project match.

	var contentJSON []byte
	if req.Content != nil {
		b, err := json.Marshal(req.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal content: %w", err)
		}
		contentJSON = b
	}

	version, err := s.Q.NextArtifactVersion(ctx, toPgUUID(req.TaskID))
	if err != nil {
		return nil, fmt.Errorf("next version: %w", err)
	}

	a, err := s.Q.CreateArtifact(ctx, db.CreateArtifactParams{
		TaskID:         toPgUUID(req.TaskID),
		SlotID:         toPgNullUUID(req.SlotID),
		ExecutionID:    toPgNullUUID(req.ExecutionID),
		RunID:          toPgUUID(req.RunID),
		ArtifactType:   req.ArtifactType,
		Version:        version,
		Title:          toPgNullText(req.Title),
		Summary:        toPgNullText(req.Summary),
		Content:        contentJSON,
		FileIndexID:    toPgUUID(req.FileIndexID),
		FileSnapshotID: toPgNullUUID(req.FileSnapshotID),
		RetentionClass: pgtype.Text{String: req.RetentionClass, Valid: true},
		CreatedByID:    toPgNullUUID(req.CreatedByID),
		CreatedByType:  toPgNullText(req.CreatedByType),
	})
	if err != nil {
		return nil, fmt.Errorf("insert artifact: %w", err)
	}
	return &a, nil
}

// NextVersion returns the next version number to use for an artifact on the
// given task. Useful for callers that need the version before creating
// (e.g. dual-writes that pre-allocate a version number).
func (s *ArtifactService) NextVersion(ctx context.Context, taskID uuid.UUID) (int, error) {
	v, err := s.Q.NextArtifactVersion(ctx, toPgUUID(taskID))
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// ArtifactWithReviews bundles an Artifact with its chronological review
// history (newest first, matching ListReviewsForArtifact).
type ArtifactWithReviews struct {
	Artifact db.Artifact
	Reviews  []db.Review
}

// GetWithReviews fetches an Artifact and its review history in two queries.
func (s *ArtifactService) GetWithReviews(ctx context.Context, artifactID uuid.UUID) (*ArtifactWithReviews, error) {
	a, err := s.Q.GetArtifact(ctx, toPgUUID(artifactID))
	if err != nil {
		return nil, err
	}
	reviews, err := s.Q.ListReviewsForArtifact(ctx, toPgUUID(artifactID))
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	return &ArtifactWithReviews{Artifact: a, Reviews: reviews}, nil
}

// Package memory: service.go — MemoryService writes/reads
// memory_record rows. Vector ops stay on the Store interface (Phase 3).
//
// Hard rule (per plan §2 + user constraint): RawRef MUST point at an
// existing row in file_index / thread_context_item / message /
// artifact. Service validates existence before insert; orphans are
// not allowed.
package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrInvalidRaw signals RawRef points at a row that doesn't exist or
// the kind isn't supported.
var ErrInvalidRaw = errors.New("memory: invalid raw reference")

// ErrNotFound signals memory_record row missing.
var ErrNotFound = errors.New("memory: record not found")

// Service is the relational side of memory persistence. Vector ops go
// through Store (Phase 3); this service only touches memory_record.
type Service struct {
	Q *db.Queries
}

func NewService(q *db.Queries) *Service { return &Service{Q: q} }

// AppendInput collapses caller args. ID is allocated by the DB.
type AppendInput struct {
	WorkspaceID uuid.UUID
	Type        MemoryType
	Scope       MemoryScope
	Source      string
	Raw         RawRef
	Summary     string
	Body        string
	Tags        []string
	Entities    []string
	Confidence  float64
	Status      MemoryStatus // default candidate
	CreatedBy   uuid.UUID
}

// Append writes one memory_record. Status defaults to candidate (per
// reference §七.4 — agents write candidates, humans confirm).
func (s *Service) Append(ctx context.Context, in AppendInput) (Memory, error) {
	if in.Raw.ID == uuid.Nil {
		return Memory{}, fmt.Errorf("%w: nil raw id", ErrInvalidRaw)
	}
	if err := s.validateRaw(ctx, in.Raw); err != nil {
		return Memory{}, err
	}
	if in.Status == "" {
		in.Status = StatusCandidate
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	if in.Entities == nil {
		in.Entities = []string{}
	}
	row, err := s.Q.CreateMemoryRecord(ctx, db.CreateMemoryRecordParams{
		WorkspaceID: pgUUID(in.WorkspaceID),
		Type:        string(in.Type),
		Scope:       string(in.Scope),
		Source:      in.Source,
		RawKind:     string(in.Raw.Kind),
		RawID:       pgUUID(in.Raw.ID),
		Summary:     pgText(in.Summary),
		Body:        pgText(in.Body),
		Tags:        in.Tags,
		Entities:    in.Entities,
		Confidence:  float32(in.Confidence),
		Status:      string(in.Status),
		Version:     1,
		CreatedBy:   pgUUID(in.CreatedBy),
	})
	if err != nil {
		return Memory{}, fmt.Errorf("create memory_record: %w", err)
	}
	return rowToMemory(row), nil
}

// Promote moves status from candidate → confirmed. Bumps version.
// Idempotent: re-promoting a confirmed row keeps it confirmed (just
// bumps version + updated_at).
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (Memory, error) {
	row, err := s.Q.PromoteMemoryRecord(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, fmt.Errorf("promote: %w", err)
	}
	return rowToMemory(row), nil
}

// Archive moves status to archived (read-only). Reverse-able only by
// a manual UPDATE — no Unarchive method to keep the API narrow.
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (Memory, error) {
	row, err := s.Q.ArchiveMemoryRecord(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, fmt.Errorf("archive: %w", err)
	}
	return rowToMemory(row), nil
}

// ListFilter narrows ListByWorkspace. Empty values mean no narrowing.
type ListFilter struct {
	Type   MemoryType
	Scope  MemoryScope
	Status MemoryStatus
	Limit  int
	Offset int
}

// ListByWorkspace returns rows in updated_at DESC order.
func (s *Service) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, f ListFilter) ([]Memory, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := s.Q.ListMemoryRecordsByWorkspace(ctx, db.ListMemoryRecordsByWorkspaceParams{
		WorkspaceID:   pgUUID(workspaceID),
		TypeFilter:    pgTextOptional(string(f.Type)),
		ScopeFilter:   pgTextOptional(string(f.Scope)),
		StatusFilter:  pgTextOptional(string(f.Status)),
		LimitCount:    int32(f.Limit),
		OffsetCount:   int32(f.Offset),
	})
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	out := make([]Memory, len(rows))
	for i, r := range rows {
		out[i] = rowToMemory(r)
	}
	return out, nil
}

// GetByRaw returns every memory_record pointing at one raw row. Useful
// for "what did agents derive from this file/transcript?" queries.
func (s *Service) GetByRaw(ctx context.Context, ref RawRef) ([]Memory, error) {
	rows, err := s.Q.ListMemoryRecordsByRaw(ctx, db.ListMemoryRecordsByRawParams{
		RawKind: string(ref.Kind),
		RawID:   pgUUID(ref.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("list by raw: %w", err)
	}
	out := make([]Memory, len(rows))
	for i, r := range rows {
		out[i] = rowToMemory(r)
	}
	return out, nil
}

// Get returns a single record by id.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Memory, error) {
	row, err := s.Q.GetMemoryRecord(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, err
	}
	return rowToMemory(row), nil
}

// validateRaw checks the referenced row exists. Polymorphic — branches
// per RawKind. Returns ErrInvalidRaw if missing or unknown kind.
func (s *Service) validateRaw(ctx context.Context, ref RawRef) error {
	switch ref.Kind {
	case RawFileIndex:
		_, err := s.Q.GetFileIndex(ctx, pgUUID(ref.ID))
		return wrapRawErr(err, ref)
	case RawThreadContextItem:
		_, err := s.Q.GetThreadContextItem(ctx, pgUUID(ref.ID))
		return wrapRawErr(err, ref)
	case RawArtifact:
		_, err := s.Q.GetArtifact(ctx, pgUUID(ref.ID))
		return wrapRawErr(err, ref)
	case RawMessage:
		_, err := s.Q.GetMessage(ctx, pgUUID(ref.ID))
		return wrapRawErr(err, ref)
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidRaw, ref.Kind)
	}
}

func wrapRawErr(err error, ref RawRef) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s/%s not found", ErrInvalidRaw, ref.Kind, ref.ID)
	}
	return fmt.Errorf("validate raw: %w", err)
}

// --- helpers (local to memory pkg) ---

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// pgTextOptional returns Valid=false on empty so the SQL filter sees NULL.
func pgTextOptional(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func rowToMemory(r db.MemoryRecord) Memory {
	return Memory{
		ID:          uuid.UUID(r.ID.Bytes),
		WorkspaceID: uuid.UUID(r.WorkspaceID.Bytes),
		Type:        MemoryType(r.Type),
		Scope:       MemoryScope(r.Scope),
		Source:      r.Source,
		Raw: RawRef{
			Kind: RawKind(r.RawKind),
			ID:   uuid.UUID(r.RawID.Bytes),
		},
		Summary:    r.Summary.String,
		Body:       r.Body.String,
		Tags:       r.Tags,
		Entities:   r.Entities,
		Confidence: float64(r.Confidence),
		Status:     MemoryStatus(r.Status),
		Version:    int(r.Version),
		CreatedBy:  uuid.UUID(r.CreatedBy.Bytes),
		CreatedAt:  r.CreatedAt.Time,
		UpdatedAt:  r.UpdatedAt.Time,
	}
}

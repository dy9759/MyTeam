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
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/embed"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// Event type constants. Bus subscribers (cloud-sync handler, audit
// writer, future analytics) match on these strings.
const (
	EventMemoryAppended  = "memory.appended"
	EventMemoryConfirmed = "memory.confirmed"
	EventMemoryArchived  = "memory.archived"
)

// ErrInvalidRaw signals RawRef points at a row that doesn't exist or
// the kind isn't supported.
var ErrInvalidRaw = errors.New("memory: invalid raw reference")

// ErrNotFound signals memory_record row missing.
var ErrNotFound = errors.New("memory: record not found")

// Service is the relational side of memory persistence. When Chunker +
// Embedder + Store are wired (WithIndexing), Promote also chunks the
// confirmed Body, embeds, and upserts into the vector store. All three
// must be wired together; nil any one disables auto-indexing.
type Service struct {
	Q        *db.Queries
	Chunker  Chunker
	Embedder embed.Embedder
	Store    Store
	Bus      *events.Bus // optional; nil disables event emission
}

func NewService(q *db.Queries) *Service { return &Service{Q: q} }

// WithIndexing enables auto-chunk + embed + upsert on Promote. Pass
// any nil to disable. Chunker chooses how to split; Embedder turns
// chunks into vectors; Store persists them. Phase 3 supplies the
// concrete impls.
func (s *Service) WithIndexing(c Chunker, e embed.Embedder, st Store) *Service {
	s.Chunker = c
	s.Embedder = e
	s.Store = st
	return s
}

// WithBus enables event emission on Append/Promote/Archive. Production
// wires the same Bus the rest of the platform uses; tests pass nil
// (default) or events.New() with subscribers.
func (s *Service) WithBus(b *events.Bus) *Service {
	s.Bus = b
	return s
}

// emit publishes an event when Bus is wired. Safe to call with nil.
func (s *Service) emit(eventType string, mem Memory) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: mem.WorkspaceID.String(),
		ActorType:   "system",
		ActorID:     mem.CreatedBy.String(),
		Payload: map[string]any{
			"memory_id": mem.ID.String(),
			"type":      string(mem.Type),
			"scope":     string(mem.Scope),
			"status":    string(mem.Status),
			"raw_kind":  string(mem.Raw.Kind),
			"raw_id":    mem.Raw.ID.String(),
			"version":   mem.Version,
			"summary":   mem.Summary,
		},
	})
}

// indexingWired returns true when all 3 dependencies are present.
func (s *Service) indexingWired() bool {
	return s.Chunker != nil && s.Embedder != nil && s.Store != nil
}

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
	mem := rowToMemory(row)
	s.emit(EventMemoryAppended, mem)
	return mem, nil
}

// Promote moves status from candidate → confirmed. Bumps version.
// Idempotent: re-promoting a confirmed row keeps it confirmed (just
// bumps version + updated_at).
//
// Auto-indexing: when WithIndexing was wired, Promote also chunks the
// memory's Summary+Body, embeds each chunk, and upserts into Store.
// Existing chunks for this memory_id are deleted first so re-promote
// is idempotent (e.g. after a Body edit). Indexing failures log a
// warning but do NOT fail the promote — the memory is confirmed
// regardless; index can be rebuilt by a follow-up call.
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (Memory, error) {
	row, err := s.Q.PromoteMemoryRecord(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, ErrNotFound
		}
		return Memory{}, fmt.Errorf("promote: %w", err)
	}
	mem := rowToMemory(row)

	if s.indexingWired() {
		if err := s.indexMemory(ctx, mem); err != nil {
			// Warn-only — promote already committed; caller still
			// gets the confirmed Memory back.
			slog.Warn("memory: auto-index on promote failed",
				"memory_id", mem.ID, "err", err)
		}
	}
	// Emit memory.confirmed so cloud-sync handler / audit writer /
	// future analytics can react. Per user reference doc §三 ("通过
	// 事件总线同步"), confirmed is the gate that lets a private_local
	// memory escalate to shared_summary on the cloud side.
	s.emit(EventMemoryConfirmed, mem)
	return mem, nil
}

// indexMemory chunks Summary+Body, embeds, and upserts. Replaces any
// existing chunks under this memory_id so re-promote is safe.
func (s *Service) indexMemory(ctx context.Context, mem Memory) error {
	source := mem.Summary
	if mem.Body != "" {
		if source != "" {
			source += "\n\n"
		}
		source += mem.Body
	}
	if source == "" {
		return nil // nothing to index
	}
	chunks := s.Chunker.Split(source)
	if len(chunks) == 0 {
		return nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vecs, err := s.Embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	if len(vecs) != len(chunks) {
		return fmt.Errorf("embedder returned %d vectors, want %d", len(vecs), len(chunks))
	}
	model := s.Embedder.Model()
	dim := s.Embedder.Dim()
	for i := range chunks {
		chunks[i].MemoryID = mem.ID
		chunks[i].Embedding = vecs[i]
		chunks[i].Model = model
		chunks[i].Dim = dim
	}
	// Atomic: prior chunks survive if Insert fails — search never sees
	// a confirmed memory with zero chunks. Issue #65.
	if err := s.Store.ReplaceByMemory(ctx, mem.ID, chunks); err != nil {
		return fmt.Errorf("replace chunks: %w", err)
	}
	return nil
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
	mem := rowToMemory(row)
	s.emit(EventMemoryArchived, mem)
	return mem, nil
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

// SearchInput collapses caller args for semantic search.
type SearchInput struct {
	WorkspaceID uuid.UUID
	Query       string
	TopK        int
	Types       []MemoryType
	Scopes      []MemoryScope
	StatusOnly  []MemoryStatus
}

// Search runs a semantic similarity query: embed the query → vector
// Store.Search → return Hits. Requires WithIndexing wired (else
// returns ErrIndexingNotWired).
func (s *Service) Search(ctx context.Context, in SearchInput) ([]Hit, error) {
	if !s.indexingWired() {
		return nil, ErrIndexingNotWired
	}
	if in.Query == "" {
		return nil, fmt.Errorf("memory.Search: query required")
	}
	vecs, err := s.Embedder.Embed(ctx, []string{in.Query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedder returned no vector")
	}
	return s.Store.Search(ctx, vecs[0], in.TopK, Filter{
		WorkspaceID: in.WorkspaceID,
		Types:       in.Types,
		Scopes:      in.Scopes,
		StatusOnly:  in.StatusOnly,
	})
}

// ErrIndexingNotWired signals Chunker/Embedder/Store missing.
var ErrIndexingNotWired = errors.New("memory: indexing not wired (call WithIndexing)")

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

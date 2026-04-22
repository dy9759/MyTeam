package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
)

// fakeEmbedder + fakeStore exercise the WithIndexing wiring without a
// real upstream / pgvector. We only need to verify Promote calls them.
type fakeEmbedder struct {
	dim   int
	model string
	calls int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, f.dim)
		v[0] = float32(i + 1) // distinguishable per-text
		out[i] = v
	}
	return out, nil
}
func (f *fakeEmbedder) Dim() int      { return f.dim }
func (f *fakeEmbedder) Model() string { return f.model }

type fakeStore struct {
	upsertedChunks [][]Chunk
	deletedMems    []uuid.UUID
	replacedMems   []uuid.UUID
}

func (f *fakeStore) Upsert(_ context.Context, chunks []Chunk) error {
	f.upsertedChunks = append(f.upsertedChunks, chunks)
	return nil
}
func (f *fakeStore) Search(_ context.Context, _ []float32, _ int, _ Filter) ([]Hit, error) {
	return nil, nil
}
func (f *fakeStore) DeleteByMemory(_ context.Context, id uuid.UUID) error {
	f.deletedMems = append(f.deletedMems, id)
	return nil
}
func (f *fakeStore) ReplaceByMemory(_ context.Context, id uuid.UUID, chunks []Chunk) error {
	f.replacedMems = append(f.replacedMems, id)
	f.upsertedChunks = append(f.upsertedChunks, chunks)
	return nil
}

func TestPromoteAutoIndexes(t *testing.T) {
	q := newTestQ(t)
	ctx := context.Background()

	wsID, userID, fileID := seedFile(t, q)
	emb := &fakeEmbedder{dim: 1024, model: "text-embedding-v4"}
	store := &fakeStore{}
	svc := NewService(q).WithIndexing(NewMarkdownChunker(), emb, store)

	// Body large enough to chunk into > 1 piece.
	body := "# Goals\n" + strings.Repeat("ship phase 4. ", 200) +
		"\n# Risks\n" + strings.Repeat("late integration. ", 200)

	mem, err := svc.Append(ctx, AppendInput{
		WorkspaceID: wsID,
		Type:        TypeSummary,
		Scope:       ScopeSharedSummary,
		Source:      "test",
		Raw:         RawRef{Kind: RawFileIndex, ID: fileID},
		Summary:     "Phase 4 review",
		Body:        body,
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if emb.calls != 0 {
		t.Errorf("Append should NOT trigger embedding (only Promote does), got %d calls", emb.calls)
	}

	confirmed, err := svc.Promote(ctx, mem.ID)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if confirmed.Status != StatusConfirmed {
		t.Errorf("status: want confirmed, got %s", confirmed.Status)
	}
	if emb.calls != 1 {
		t.Errorf("expected 1 embed call on Promote, got %d", emb.calls)
	}
	if len(store.replacedMems) != 1 || store.replacedMems[0] != mem.ID {
		t.Errorf("expected ReplaceByMemory(%s), got %#v", mem.ID, store.replacedMems)
	}
	if len(store.upsertedChunks) != 1 {
		t.Fatalf("expected 1 Upsert batch, got %d", len(store.upsertedChunks))
	}
	chunks := store.upsertedChunks[0]
	if len(chunks) < 2 {
		t.Errorf("expected >=2 chunks for large body, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.MemoryID != mem.ID {
			t.Errorf("chunk MemoryID mismatch: %s != %s", c.MemoryID, mem.ID)
		}
		if c.Model != "text-embedding-v4" || c.Dim != 1024 {
			t.Errorf("chunk model/dim: %s/%d", c.Model, c.Dim)
		}
		if len(c.Embedding) != 1024 {
			t.Errorf("chunk embedding len %d", len(c.Embedding))
		}
	}

	// Re-promote → idempotent (calls embedder again, replaces chunks).
	if _, err := svc.Promote(ctx, mem.ID); err != nil {
		t.Fatalf("re-Promote: %v", err)
	}
	if emb.calls != 2 {
		t.Errorf("re-promote: want 2 embed calls total, got %d", emb.calls)
	}
	if len(store.replacedMems) != 2 {
		t.Errorf("re-promote: want 2 ReplaceByMemory calls, got %d", len(store.replacedMems))
	}
}

func TestPromoteWithoutIndexing_NoCalls(t *testing.T) {
	q := newTestQ(t)
	ctx := context.Background()
	wsID, userID, fileID := seedFile(t, q)
	svc := NewService(q) // no WithIndexing

	mem, err := svc.Append(ctx, AppendInput{
		WorkspaceID: wsID,
		Type:        TypeFact,
		Scope:       ScopeSharedSummary,
		Source:      "test",
		Raw:         RawRef{Kind: RawFileIndex, ID: fileID},
		Summary:     "no-index",
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	confirmed, err := svc.Promote(ctx, mem.ID)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if confirmed.Status != StatusConfirmed {
		t.Errorf("status: want confirmed, got %s", confirmed.Status)
	}
}

func TestSearch_RequiresIndexing(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Search(context.Background(), SearchInput{
		WorkspaceID: uuid.New(),
		Query:       "x",
	})
	if err != ErrIndexingNotWired {
		t.Fatalf("want ErrIndexingNotWired, got %v", err)
	}
}

func TestService_EmitsBusEventsOnLifecycle(t *testing.T) {
	q := newTestQ(t)
	ctx := context.Background()
	wsID, userID, fileID := seedFile(t, q)

	bus := events.New()
	var seen []events.Event
	bus.SubscribeAll(func(e events.Event) { seen = append(seen, e) })

	svc := NewService(q).WithBus(bus)

	mem, err := svc.Append(ctx, AppendInput{
		WorkspaceID: wsID,
		Type:        TypeFact,
		Scope:       ScopeSharedSummary,
		Source:      "test",
		Raw:         RawRef{Kind: RawFileIndex, ID: fileID},
		Summary:     "bus test",
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := svc.Promote(ctx, mem.ID); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if _, err := svc.Archive(ctx, mem.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	wantTypes := []string{EventMemoryAppended, EventMemoryConfirmed, EventMemoryArchived}
	if len(seen) != 3 {
		t.Fatalf("expected 3 events, got %d (%+v)", len(seen), seen)
	}
	for i, want := range wantTypes {
		if seen[i].Type != want {
			t.Errorf("event %d: want type %s, got %s", i, want, seen[i].Type)
		}
		if seen[i].WorkspaceID != wsID.String() {
			t.Errorf("event %d: workspace_id mismatch", i)
		}
		payload, ok := seen[i].Payload.(map[string]any)
		if !ok || payload["memory_id"] != mem.ID.String() {
			t.Errorf("event %d: payload memory_id mismatch (%+v)", i, seen[i].Payload)
		}
	}
}

func TestService_NilBus_NoEmit(t *testing.T) {
	q := newTestQ(t)
	wsID, userID, fileID := seedFile(t, q)
	// Bus nil — Append must not panic.
	svc := NewService(q)
	_, err := svc.Append(context.Background(), AppendInput{
		WorkspaceID: wsID,
		Type:        TypeFact,
		Scope:       ScopeSharedSummary,
		Source:      "test",
		Raw:         RawRef{Kind: RawFileIndex, ID: fileID},
		Summary:     "nil bus",
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
}

// memory_search_test.go — HTTP-level e2e for /api/memories/search.
// Covers the path the daemon actually uses: REST → handler → service →
// (chunker + embedder + store) → Hits. Bridges the gap between
// internal/mcp/tools/memory_search_e2e_test.go (in-process tool exec)
// and internal/service/memory/pgvector_store_test.go (store unit).
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
)

// fakeIndexEmbedder satisfies embed.Embedder. Returns a deterministic
// 1024-vec where the first slot encodes the call index — search hits
// match by exact vec equality (see fakeIndexStore.Search).
type fakeIndexEmbedder struct{}

func (fakeIndexEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, 1024)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}
func (fakeIndexEmbedder) Dim() int      { return 1024 }
func (fakeIndexEmbedder) Model() string { return "fake-1024" }

// fakeIndexStore satisfies memory.Store. In-memory chunk slice with a
// trivial cosine-equality search so we can assert hits without a real
// pgvector backend.
type fakeIndexStore struct {
	chunks []memory.Chunk
}

func (s *fakeIndexStore) Upsert(_ context.Context, chunks []memory.Chunk) error {
	s.chunks = append(s.chunks, chunks...)
	return nil
}
func (s *fakeIndexStore) Search(_ context.Context, embedding []float32, topK int, _ memory.Filter) ([]memory.Hit, error) {
	hits := []memory.Hit{}
	if len(embedding) == 0 {
		return hits, nil
	}
	for _, c := range s.chunks {
		if len(c.Embedding) > 0 && c.Embedding[0] == embedding[0] {
			hits = append(hits, memory.Hit{Chunk: c, Score: 1.0})
			if topK > 0 && len(hits) >= topK {
				break
			}
		}
	}
	return hits, nil
}
func (s *fakeIndexStore) DeleteByMemory(_ context.Context, memoryID uuid.UUID) error {
	kept := s.chunks[:0]
	for _, c := range s.chunks {
		if c.MemoryID != memoryID {
			kept = append(kept, c)
		}
	}
	s.chunks = kept
	return nil
}
func (s *fakeIndexStore) ReplaceByMemory(ctx context.Context, memoryID uuid.UUID, chunks []memory.Chunk) error {
	if err := s.DeleteByMemory(ctx, memoryID); err != nil {
		return err
	}
	return s.Upsert(ctx, chunks)
}

// withMemoryIndexing swaps testHandler.Memory to a service WithIndexing
// wired for the test, then restores the original on cleanup.
func withMemoryIndexing(t *testing.T) {
	t.Helper()
	prev := testHandler.Memory
	svc := memory.NewService(testHandler.Queries).
		WithIndexing(memory.NewMarkdownChunker(), fakeIndexEmbedder{}, &fakeIndexStore{})
	testHandler.Memory = svc
	t.Cleanup(func() { testHandler.Memory = prev })
}

// seedRawFile inserts one file_index row so memory.Append's RawRef
// validation passes. Returns the file id as string for the request body.
func seedRawFile(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var fileID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO file_index (
			workspace_id, uploader_identity_id, uploader_identity_type, owner_id,
			source_type, source_id, file_name, file_size, content_type, storage_path, access_scope
		)
		VALUES ($1, $2, 'member', $2, 'channel', $1, $3, 1, 'text/markdown', $4, '{"scope":"channel"}'::jsonb)
		RETURNING id
	`, testWorkspaceID, testUserID, "search-e2e.md", "/tmp/search-e2e.md").Scan(&fileID); err != nil {
		t.Fatalf("insert file_index: %v", err)
	}
	id := uuidToString(fileID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM file_index WHERE id = $1`, id)
	})
	return id
}

// TestSearchMemories_HappyPath — Append → Promote (triggers indexing) →
// Search returns hits. Exercises the full HTTP surface end-to-end.
func TestSearchMemories_HappyPath(t *testing.T) {
	withMemoryIndexing(t)
	rawID := seedRawFile(t)

	// 1. Append a candidate memory.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memories", map[string]any{
		"type":     "summary",
		"scope":    "shared_summary",
		"source":   "search-e2e",
		"raw_kind": "file_index",
		"raw_id":   rawID,
		"summary":  "phase V e2e",
		"body":     "# Goal\nverify HTTP search path.",
	})
	testHandler.CreateMemory(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMemory: %d %s", w.Code, w.Body.String())
	}
	var created memory.Memory
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM memory_record WHERE id = $1`, created.ID.String())
	})

	// 2. Promote — this invokes indexMemory (chunker + embedder + store).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memories/"+created.ID.String()+"/promote", nil)
	req = withURLParam(req, "id", created.ID.String())
	testHandler.PromoteMemory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PromoteMemory: %d %s", w.Code, w.Body.String())
	}

	// 3. Search — fake embedder returns vec[0]=1; fake store matches by
	// vec[0] equality so the upsert from step 2 is now findable.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/memories/search", map[string]any{
		"query": "verify search",
		"top_k": 5,
	})
	testHandler.SearchMemories(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchMemories: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(resp.Hits) < 1 {
		t.Fatalf("expected >= 1 hit, got %d (body=%s)", len(resp.Hits), w.Body.String())
	}
}

// TestSearchMemories_503WhenIndexingNotWired — base Memory svc has no
// indexing; Search must return 503 service_unavailable, not 500.
func TestSearchMemories_503WhenIndexingNotWired(t *testing.T) {
	prev := testHandler.Memory
	testHandler.Memory = memory.NewService(testHandler.Queries) // no WithIndexing
	t.Cleanup(func() { testHandler.Memory = prev })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memories/search", map[string]any{"query": "x"})
	testHandler.SearchMemories(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSearchMemories_400OnEmptyQuery — empty query returns 400, not 500.
func TestSearchMemories_400OnEmptyQuery(t *testing.T) {
	withMemoryIndexing(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/memories/search", map[string]any{"query": ""})
	testHandler.SearchMemories(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

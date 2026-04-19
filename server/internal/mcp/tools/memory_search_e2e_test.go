package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	"github.com/multica-ai/multica/server/internal/service/memory"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeEmbedder struct {
	calls int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, 1024)
		vec[0] = 1
		out[i] = vec
	}
	return out, nil
}

func (f *fakeEmbedder) Dim() int { return 1024 }

func (f *fakeEmbedder) Model() string { return "fake-1024" }

type fakeStore struct {
	chunks []memory.Chunk
}

func (f *fakeStore) Upsert(_ context.Context, chunks []memory.Chunk) error {
	f.chunks = append(f.chunks, chunks...)
	return nil
}

func (f *fakeStore) Search(_ context.Context, embedding []float32, topK int, _ memory.Filter) ([]memory.Hit, error) {
	hits := []memory.Hit{}
	if len(embedding) == 0 {
		return hits, nil
	}
	for _, chunk := range f.chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		diff := chunk.Embedding[0] - embedding[0]
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.01 {
			continue
		}
		hits = append(hits, memory.Hit{
			Chunk: chunk,
			Score: 1 - float64(diff),
		})
		if topK > 0 && len(hits) >= topK {
			break
		}
	}
	return hits, nil
}

func (f *fakeStore) DeleteByMemory(_ context.Context, memoryID uuid.UUID) error {
	kept := f.chunks[:0]
	for _, chunk := range f.chunks {
		if chunk.MemoryID != memoryID {
			kept = append(kept, chunk)
		}
	}
	f.chunks = kept
	return nil
}

func TestMemorySearch_E2E_AppendPromoteSearch(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	q := db.New(pool)
	workspaceID, userID, fileID := seedMemorySearchE2EFixture(t, ctx, pool)

	embedder := &fakeEmbedder{}
	store := &fakeStore{}
	memoryService := memory.NewService(q).WithIndexing(memory.NewMarkdownChunker(), embedder, store)
	mcpCtx := mcptool.Context{
		WorkspaceID: workspaceID,
		UserID:      userID,
		RuntimeMode: mcptool.RuntimeCloud,
		Memory:      memoryService,
	}

	appendResult, err := (MemoryAppend{}).Exec(ctx, q, mcpCtx, map[string]any{
		"type":     "summary",
		"scope":    "shared_summary",
		"source":   "e2e",
		"raw_kind": "file_index",
		"raw_id":   fileID.String(),
		"summary":  "phase G review",
		"body":     "# Goals\nship indexing.\n# Risks\nlate.",
	})
	if err != nil {
		t.Fatalf("memory_append exec: %v", err)
	}
	requireNoMemoryToolErrors(t, "memory_append", appendResult)

	appendData := resultDataAsMap(t, appendResult.Data)
	memoryData := nestedResultMap(t, appendData, "memory")
	memoryIDRaw, ok := memoryData["id"].(string)
	if !ok || memoryIDRaw == "" {
		t.Fatalf("memory_append missing memory.id: %#v", memoryData)
	}
	memoryID, err := uuid.Parse(memoryIDRaw)
	if err != nil {
		t.Fatalf("parse memory id %q: %v", memoryIDRaw, err)
	}

	promoteResult, err := (MemoryPromote{}).Exec(ctx, q, mcpCtx, map[string]any{
		"memory_id": memoryID.String(),
	})
	if err != nil {
		t.Fatalf("memory_promote exec: %v", err)
	}
	requireNoMemoryToolErrors(t, "memory_promote", promoteResult)

	promoteData := resultDataAsMap(t, promoteResult.Data)
	promotedMemory := nestedResultMap(t, promoteData, "memory")
	if got := promotedMemory["status"]; got != "confirmed" {
		t.Fatalf("promoted memory status = %v, want confirmed; data=%#v", got, promotedMemory)
	}
	if embedder.calls != 1 {
		t.Fatalf("fake embedder calls after promote = %d, want 1", embedder.calls)
	}
	if len(store.chunks) < 1 {
		t.Fatalf("fake store chunks after promote = %d, want >= 1", len(store.chunks))
	}

	searchResult, err := (MemorySearch{}).Exec(ctx, q, mcpCtx, map[string]any{
		"query": "ship indexing",
	})
	if err != nil {
		t.Fatalf("memory_search exec: %v", err)
	}
	requireNoMemoryToolErrors(t, "memory_search", searchResult)

	searchData := resultDataAsMap(t, searchResult.Data)
	hits, ok := searchData["hits"].([]any)
	if !ok {
		t.Fatalf("memory_search hits has type %T, want []any; data=%#v", searchData["hits"], searchData)
	}
	if len(hits) < 1 {
		t.Fatalf("memory_search returned %d hits, want >= 1", len(hits))
	}

	listResult, err := (MemoryList{}).Exec(ctx, q, mcpCtx, map[string]any{})
	if err != nil {
		t.Fatalf("memory_list exec: %v", err)
	}
	requireNoMemoryToolErrors(t, "memory_list", listResult)

	listData := resultDataAsMap(t, listResult.Data)
	memories, ok := listData["memories"].([]any)
	if !ok {
		t.Fatalf("memory_list memories has type %T, want []any; data=%#v", listData["memories"], listData)
	}
	if len(memories) < 1 {
		t.Fatalf("memory_list returned %d memories, want >= 1", len(memories))
	}
}

func seedMemorySearchE2EFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	suffix := uuid.NewString()
	email := "memory-search-e2e+" + suffix + "@example.com"
	slug := "memory-search-e2e-" + suffix

	var userID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Memory Search E2E", email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Memory Search E2E", slug, "memory search e2e workspace", "MSE").Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
			t.Logf("cleanup workspace: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	var fileID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO file_index (
			workspace_id, uploader_identity_id, uploader_identity_type, owner_id,
			source_type, source_id, file_name, file_size, content_type, storage_path, access_scope
		)
		VALUES ($1, $2, 'member', $2, 'channel', $1, $3, 1, 'text/markdown', $4, '{"scope":"channel"}'::jsonb)
		RETURNING id
	`, workspaceID, userID, "memory-search-e2e.md", "/tmp/memory-search-e2e.md").Scan(&fileID); err != nil {
		t.Fatalf("insert file_index: %v", err)
	}

	return pgUUIDToUUID(t, workspaceID), pgUUIDToUUID(t, userID), pgUUIDToUUID(t, fileID)
}

func requireNoMemoryToolErrors(t *testing.T, name string, result mcptool.Result) {
	t.Helper()
	if len(result.Errors) > 0 {
		t.Fatalf("%s returned errors: %v note=%q data=%#v", name, result.Errors, result.Note, result.Data)
	}
}

func resultDataAsMap(t *testing.T, data any) map[string]any {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal result data: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal result data %s: %v", string(b), err)
	}
	return out
}

func nestedResultMap(t *testing.T, data map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := data[key].(map[string]any)
	if !ok {
		t.Fatalf("result data %q has type %T, want map[string]any; data=%#v", key, data[key], data)
	}
	return nested
}

func pgUUIDToUUID(t *testing.T, id pgtype.UUID) uuid.UUID {
	t.Helper()
	if !id.Valid {
		t.Fatalf("invalid pg UUID")
	}
	return uuid.UUID(id.Bytes)
}

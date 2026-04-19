package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestPoster_NoOpWhenURLEmpty — empty url disables Enqueue + Start +
// Stop without panic. Matches the "MEMORY_HUB_URL not set" semantics
// that production env-defaults to.
func TestPoster_NoOpWhenURLEmpty(t *testing.T) {
	p := newMemoryHubPoster("", "", 4, 16)
	p.Start(context.Background()) // must not spawn workers
	if got := p.Enqueue("memory.confirmed", "ws-1", map[string]any{"memory_id": "x"}); got {
		t.Fatal("Enqueue should return false when url empty")
	}
	p.Stop(time.Second)
}

// TestPoster_PostsWithIdempotencyHeader — full happy path: start, enqueue,
// upstream sees POST + Authorization + Idempotency-Key + body shape, stop
// drains worker.
func TestPoster_PostsWithIdempotencyHeader(t *testing.T) {
	var got struct {
		method  string
		path    string
		auth    string
		idemKey string
		body    map[string]any
		called  int32
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&got.called, 1)
		got.method = r.Method
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.idemKey = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	p := newMemoryHubPoster(srv.URL, "tok-T", 2, 8)
	p.Start(context.Background())
	t.Cleanup(func() { p.Stop(2 * time.Second) })

	if !p.Enqueue("memory.confirmed", "ws-9",
		map[string]any{"memory_id": "abc", "scope": "team"}) {
		t.Fatal("Enqueue rejected")
	}

	// Wait briefly for worker to consume + post.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&got.called) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&got.called) == 0 {
		t.Fatal("upstream never called")
	}

	if got.method != "POST" || got.path != "/api/v1/memories" {
		t.Errorf("path/method: %s %s", got.method, got.path)
	}
	if got.auth != "Bearer tok-T" {
		t.Errorf("Authorization: %q", got.auth)
	}
	if got.idemKey != "memory.confirmed:abc" {
		t.Errorf("Idempotency-Key: %q", got.idemKey)
	}
	if got.body["event_type"] != "memory.confirmed" || got.body["workspace_id"] != "ws-9" {
		t.Errorf("body: %#v", got.body)
	}
}

// TestPoster_DropsWhenQueueFull — depth=1, no Start, fill once, second
// Enqueue must return false (drop, no panic, no goroutine leak).
func TestPoster_DropsWhenQueueFull(t *testing.T) {
	p := newMemoryHubPoster("http://stub.invalid", "", 1, 1)
	// Don't Start — workers never drain.
	if !p.Enqueue("memory.confirmed", "ws", map[string]any{"memory_id": "1"}) {
		t.Fatal("first Enqueue should accept")
	}
	if p.Enqueue("memory.confirmed", "ws", map[string]any{"memory_id": "2"}) {
		t.Fatal("second Enqueue should drop (queue full)")
	}
}

// TestPoster_StopDrainsBufferedEvents — workers running; enqueue several;
// Stop closes queue and remaining events finish before Stop returns
// (within timeout). Verifies the drain branch in runWorker.
func TestPoster_StopDrainsBufferedEvents(t *testing.T) {
	var seen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&seen, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newMemoryHubPoster(srv.URL, "", 2, 16)
	p.Start(context.Background())
	for i := 0; i < 5; i++ {
		p.Enqueue("memory.confirmed", "ws", map[string]any{"memory_id": "m"})
	}
	p.Stop(3 * time.Second)
	if got := atomic.LoadInt32(&seen); got != 5 {
		t.Errorf("expected 5 posts, got %d", got)
	}
}

// TestPoster_StartIdempotent / StopIdempotent — defense against double-
// boot during graceful restart.
func TestPoster_StartStopIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newMemoryHubPoster(srv.URL, "", 2, 4)
	p.Start(context.Background())
	p.Start(context.Background()) // no panic, no extra workers
	p.Stop(time.Second)
	p.Stop(time.Second) // no panic
}

// memory_hub_poster.go — bounded worker pool for outbound MyMemo Hub
// POSTs. Replaces the previous unbounded `go postToMemoryHub(...)` fire-
// and-forget so a slow upstream + event flood (e.g. mass auto-promote on
// meeting summarize) cannot stack thousands of in-flight goroutines.
//
// Design:
//
//	Enqueue (non-blocking) → buffered chan → N workers consume → POST.
//	Full chan = drop + warn (no back-pressure into bus).
//	Stop = close chan, drain in worker, return after timeout.
//
// Why a struct + default singleton (not free funcs): tests need a fresh
// instance with controllable queue depth + worker count without leaking
// goroutines across tests. Production wires `defaultMemoryHubPoster`
// once at boot.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// memoryHubEvent is one queued POST.
type memoryHubEvent struct {
	eventType   string
	workspaceID string
	memoryID    string // for Idempotency-Key
	payload     map[string]any
}

// memoryHubPoster owns the worker pool + outbound HTTP client.
type memoryHubPoster struct {
	url     string
	bearer  string
	client  *http.Client
	queue   chan memoryHubEvent
	workers int

	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
	quit      chan struct{}
}

// newMemoryHubPoster builds a disabled-by-default poster. Call Start to
// spawn workers. Enqueue is a no-op when url=="" — matches the previous
// "MEMORY_HUB_URL not set → skip" semantics.
func newMemoryHubPoster(url, bearer string, workers, queueDepth int) *memoryHubPoster {
	if workers <= 0 {
		workers = 8
	}
	if queueDepth <= 0 {
		queueDepth = 256
	}
	return &memoryHubPoster{
		url:     url,
		bearer:  bearer,
		client:  &http.Client{Timeout: 10 * time.Second},
		queue:   make(chan memoryHubEvent, queueDepth),
		workers: workers,
		quit:    make(chan struct{}),
	}
}

// Start spawns N worker goroutines. Idempotent — second call is a no-op.
// ctx cancellation cascades into the workers so server shutdown stops
// outbound traffic cleanly.
func (p *memoryHubPoster) Start(ctx context.Context) {
	if p == nil || p.url == "" {
		return
	}
	p.startOnce.Do(func() {
		for i := 0; i < p.workers; i++ {
			p.wg.Add(1)
			go p.runWorker(ctx)
		}
		slog.Info("memory hub: poster started",
			"workers", p.workers, "queue", cap(p.queue), "url", p.url)
	})
}

// Stop closes the queue + waits for workers to drain, capped by
// timeout. After timeout the function returns even if workers are still
// processing — they exit on the next ctx tick.
func (p *memoryHubPoster) Stop(timeout time.Duration) {
	if p == nil || p.url == "" {
		return
	}
	p.stopOnce.Do(func() {
		close(p.quit)
		done := make(chan struct{})
		go func() { p.wg.Wait(); close(done) }()
		select {
		case <-done:
			slog.Info("memory hub: poster stopped cleanly")
		case <-time.After(timeout):
			slog.Warn("memory hub: poster stop timed out", "timeout", timeout)
		}
	})
}

// Enqueue is non-blocking. Drops + warns when queue is full. Caller never
// blocks — the bus stays sync. Returns true when accepted, false when
// dropped (test hook).
func (p *memoryHubPoster) Enqueue(eventType, workspaceID string, payload map[string]any) bool {
	if p == nil || p.url == "" {
		return false
	}
	memID, _ := payload["memory_id"].(string)
	ev := memoryHubEvent{
		eventType:   eventType,
		workspaceID: workspaceID,
		memoryID:    memID,
		payload:     payload,
	}
	select {
	case p.queue <- ev:
		return true
	default:
		slog.Warn("memory hub: queue full, dropping event",
			"event_type", eventType, "workspace_id", workspaceID,
			"memory_id", memID, "depth", cap(p.queue))
		return false
	}
}

// runWorker pulls events until quit fires + queue drains.
func (p *memoryHubPoster) runWorker(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-p.quit:
			// Drain remaining buffered events on shutdown.
			for {
				select {
				case ev := <-p.queue:
					p.post(ctx, ev)
				default:
					return
				}
			}
		case <-ctx.Done():
			return
		case ev := <-p.queue:
			p.post(ctx, ev)
		}
	}
}

// post does the actual HTTP call with idempotency header so upstream can
// safely dedup retries.
func (p *memoryHubPoster) post(ctx context.Context, ev memoryHubEvent) {
	body, err := json.Marshal(map[string]any{
		"event_type":   ev.eventType,
		"workspace_id": ev.workspaceID,
		"payload":      ev.payload,
		"sent_at":      time.Now().UTC(),
	})
	if err != nil {
		slog.Warn("memory hub: marshal failed", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		p.url+"/api/v1/memories", bytes.NewReader(body))
	if err != nil {
		slog.Warn("memory hub: build request failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if p.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+p.bearer)
	}
	if ev.memoryID != "" {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("%s:%s", ev.eventType, ev.memoryID))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Warn("memory hub: post failed", "url", p.url, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		slog.Warn("memory hub: non-2xx",
			"status", resp.StatusCode, "body", string(raw))
		return
	}
	slog.Info("memory hub: synced",
		"event_type", ev.eventType,
		"workspace_id", ev.workspaceID,
		"memory_id", ev.memoryID)
}

// defaultMemoryHubPoster is the singleton wired in main.go. Built lazily
// from the same env vars the legacy postToMemoryHub used.
var defaultMemoryHubPoster = newMemoryHubPoster(memoryHubURL, memoryHubBearer, 8, 256)

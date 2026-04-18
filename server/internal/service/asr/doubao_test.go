package asr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiaojiClient_BatchSummarize_HappyPath(t *testing.T) {
	var submitCalls, queryCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			submitCalls++
			if r.Header.Get("X-Api-App-Key") != "app-id-1" {
				t.Errorf("X-Api-App-Key not propagated, got %q", r.Header.Get("X-Api-App-Key"))
			}
			if r.Header.Get("X-Api-Access-Key") != "tok-1" {
				t.Errorf("X-Api-Access-Key not propagated, got %q", r.Header.Get("X-Api-Access-Key"))
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"resp":{"id":"task-42"}}`))
		case strings.HasSuffix(r.URL.Path, "/query"):
			queryCalls++
			// First query returns running, second returns success.
			if queryCalls < 2 {
				_, _ = w.Write([]byte(`{"resp":{"status":"running"}}`))
				return
			}
			payload := map[string]any{
				"resp": map[string]any{
					"status": "success",
					"result": map[string]any{
						"sections":  []string{"intro", "decisions"},
						"decisions": []string{"ship phase 1"},
						"action_items": []map[string]any{
							{"task": "draft prd", "owner": "alice", "confidence": 0.9},
							{"task": "review code", "owner": "", "confidence": 0},
						},
						"segments": []map[string]any{
							{"speaker": "A", "text": "hello", "start_ms": 0, "end_ms": 1500},
						},
					},
				},
			}
			b, _ := json.Marshal(payload)
			_, _ = w.Write(b)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &MiaojiClient{
		HTTP:       srv.Client(),
		Endpoint:   srv.URL,
		ResourceID: "volc.bigasr.auc.lark",
		PollEvery:  10 * time.Millisecond,
		PollMax:    2 * time.Second,
	}
	bundle, err := c.BatchSummarize(context.Background(),
		Credentials{AppID: "app-id-1", AccessToken: "tok-1", SecretKey: "secret-1"},
		"https://example.com/audio.mp3",
	)
	if err != nil {
		t.Fatalf("BatchSummarize: %v", err)
	}
	if submitCalls != 1 {
		t.Errorf("submit calls: want 1, got %d", submitCalls)
	}
	if queryCalls < 2 {
		t.Errorf("query calls: want >=2 (poll loop), got %d", queryCalls)
	}
	if bundle.Provider != "doubao_miaoji" {
		t.Errorf("provider: want doubao_miaoji, got %q", bundle.Provider)
	}
	if len(bundle.ActionItems) != 2 {
		t.Fatalf("action items: want 2, got %d", len(bundle.ActionItems))
	}
	if bundle.ActionItems[0].Owner != "alice" || bundle.ActionItems[0].Confidence != 0.9 {
		t.Errorf("action item 0: %+v", bundle.ActionItems[0])
	}
	if bundle.ActionItems[1].Confidence != 0.5 {
		t.Errorf("zero-confidence should default to 0.5, got %v", bundle.ActionItems[1].Confidence)
	}
	if len(bundle.Segments) != 1 || bundle.Segments[0].End != 1500*time.Millisecond {
		t.Errorf("segments: %+v", bundle.Segments)
	}
}

func TestMiaojiClient_BatchSummarize_UpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			_, _ = w.Write([]byte(`{"resp":{"id":"t1"}}`))
		case strings.HasSuffix(r.URL.Path, "/query"):
			_, _ = w.Write([]byte(`{"resp":{"status":"failed"}}`))
		}
	}))
	defer srv.Close()

	c := &MiaojiClient{
		HTTP:      srv.Client(),
		Endpoint:  srv.URL,
		PollEvery: 10 * time.Millisecond,
		PollMax:   1 * time.Second,
	}
	_, err := c.BatchSummarize(context.Background(), Credentials{AppID: "x", AccessToken: "y"}, "u")
	if err == nil || !strings.Contains(err.Error(), "upstream task failed") {
		t.Fatalf("expected upstream-task-failed error, got %v", err)
	}
}

func TestMiaojiClient_BatchSummarize_DeadlineExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			_, _ = w.Write([]byte(`{"resp":{"id":"t1"}}`))
		default:
			_, _ = w.Write([]byte(`{"resp":{"status":"running"}}`))
		}
	}))
	defer srv.Close()

	c := &MiaojiClient{
		HTTP:      srv.Client(),
		Endpoint:  srv.URL,
		PollEvery: 10 * time.Millisecond,
		PollMax:   50 * time.Millisecond,
	}
	_, err := c.BatchSummarize(context.Background(), Credentials{}, "u")
	if err != ErrUpstreamNotReady {
		t.Fatalf("expected ErrUpstreamNotReady, got %v", err)
	}
}

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

	// Sub-server hosts the result-file URLs that query points at.
	resultMux := http.NewServeMux()
	resultMux.HandleFunc("/transcription.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"utterances":[{"speaker":"A","text":"hello","start_time":0,"end_time":1500}]}`))
	})
	resultMux.HandleFunc("/information.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"todo_list":[{"task":"draft prd","owner":"alice","due_date":"2026-04-30"}],"decisions":["ship phase 1"]}`))
	})
	resultMux.HandleFunc("/summary.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"summary":"sprint review concluded with 2 actions"}`))
	})
	resultSrv := httptest.NewServer(resultMux)
	defer resultSrv.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			submitCalls++
			if r.Header.Get("X-Api-App-Key") != "app-id-1" {
				t.Errorf("X-Api-App-Key: %q", r.Header.Get("X-Api-App-Key"))
			}
			if r.Header.Get("X-Api-Sequence") != "-1" {
				t.Errorf("X-Api-Sequence missing: %q", r.Header.Get("X-Api-Sequence"))
			}
			// Verify the spec-shaped body — Input.Offline.FileURL.
			var got map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&got)
			input, _ := got["Input"].(map[string]interface{})
			offline, _ := input["Offline"].(map[string]interface{})
			if offline["FileURL"] != "https://example.com/audio.mp3" {
				t.Errorf("FileURL not in body: %#v", offline)
			}
			_, _ = w.Write([]byte(`{"TaskID":"task-42"}`))
		case strings.HasSuffix(r.URL.Path, "/query"):
			queryCalls++
			// First query returns running, second returns success.
			if queryCalls < 2 {
				_, _ = w.Write([]byte(`{"Status":"running"}`))
				return
			}
			payload := map[string]interface{}{
				"Status":           "success",
				"TranscriptionURL": resultSrv.URL + "/transcription.json",
				"InformationURL":   resultSrv.URL + "/information.json",
				"SummaryURL":       resultSrv.URL + "/summary.json",
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
		ResourceID: "volc.lark.minutes",
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
	if len(bundle.ActionItems) != 1 {
		t.Fatalf("action items: want 1, got %d (full bundle: %+v)", len(bundle.ActionItems), bundle)
	}
	if bundle.ActionItems[0].Owner != "alice" || bundle.ActionItems[0].Task != "draft prd" {
		t.Errorf("action item 0: %+v", bundle.ActionItems[0])
	}
	if len(bundle.Segments) != 1 || bundle.Segments[0].End != 1500*time.Millisecond {
		t.Errorf("segments: %+v", bundle.Segments)
	}
	if len(bundle.Decisions) != 1 || bundle.Decisions[0] != "ship phase 1" {
		t.Errorf("decisions: %+v", bundle.Decisions)
	}
}

func TestMiaojiClient_BatchSummarize_UpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			_, _ = w.Write([]byte(`{"TaskID":"t1"}`))
		case strings.HasSuffix(r.URL.Path, "/query"):
			_, _ = w.Write([]byte(`{"Status":"failed","ErrMessage":"audio unreadable"}`))
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
			_, _ = w.Write([]byte(`{"TaskID":"t1"}`))
		default:
			_, _ = w.Write([]byte(`{"Status":"running"}`))
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

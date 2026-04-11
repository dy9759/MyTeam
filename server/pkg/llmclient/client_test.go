package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChat_OpenAIFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Bearer auth
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}
		// Verify no Anthropic headers
		if v := r.Header.Get("x-api-key"); v != "" {
			t.Errorf("unexpected x-api-key header: %q", v)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		// Verify system message is first in messages array
		msgs := req["messages"].([]any)
		first := msgs[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("expected system message first, got role=%s", first["role"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from OpenAI format"}},
			},
		})
	}))
	defer srv.Close()

	client := New(Config{
		Endpoint:  srv.URL,
		APIKey:    "test-key",
		Model:     "test-model",
		MaxTokens: 100,
	})

	result, err := client.Chat(context.Background(), "You are helpful", []Message{
		{Role: "user", Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello from OpenAI format" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestChat_AnthropicFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Anthropic auth
		if v := r.Header.Get("x-api-key"); v != "test-key" {
			t.Errorf("expected x-api-key, got %q", v)
		}
		if v := r.Header.Get("anthropic-version"); v != "2023-06-01" {
			t.Errorf("expected anthropic-version, got %q", v)
		}
		// Verify no Bearer header
		if v := r.Header.Get("Authorization"); v != "" {
			t.Errorf("unexpected Authorization header: %q", v)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		// Verify system is a top-level field, not in messages
		if _, ok := req["system"]; !ok {
			t.Error("expected system field in Anthropic request")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"text": "Hello from Anthropic format"},
			},
		})
	}))
	defer srv.Close()

	// URL must contain "anthropic" for detection
	client := New(Config{
		Endpoint:  srv.URL + "/anthropic/v1/messages",
		APIKey:    "test-key",
		Model:     "test-model",
		MaxTokens: 100,
	})

	result, err := client.Chat(context.Background(), "You are helpful", []Message{
		{Role: "user", Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello from Anthropic format" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestChat_NoAPIKey(t *testing.T) {
	client := New(Config{
		Endpoint: "https://example.com",
		Model:    "test",
	})

	_, err := client.Chat(context.Background(), "", []Message{{Role: "user", Content: "Hi"}})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestChat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	client := New(Config{
		Endpoint: srv.URL,
		APIKey:   "key",
		Model:    "m",
	})

	_, err := client.Chat(context.Background(), "", []Message{{Role: "user", Content: "Hi"}})
	if err == nil {
		t.Error("expected error for 429 status")
	}
}

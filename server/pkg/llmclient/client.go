// Package llmclient provides a unified HTTP client for LLM APIs.
// It auto-detects Anthropic vs OpenAI-compatible (DashScope, etc.) endpoints.
package llmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Config configures an LLM client instance.
type Config struct {
	Endpoint    string // Full URL, e.g. "https://api.anthropic.com/v1/messages"
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature *float64 // nil = omit from request
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// Response from the LLM API.
type Response struct {
	Content string
	Model   string
}

// Client is a unified LLM HTTP client.
type Client struct {
	cfg    Config
	http   *http.Client
}

// New creates a new Client from the given config.
func New(cfg Config) *Client {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 1024
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// isAnthropic returns true if the endpoint looks like Anthropic's API.
func (c *Client) isAnthropic() bool {
	return strings.Contains(c.cfg.Endpoint, "anthropic")
}

// Chat sends a chat completion request and returns the response text.
// system is the system prompt (can be empty). messages is the conversation.
func (c *Client) Chat(ctx context.Context, system string, messages []Message) (string, error) {
	if c.cfg.APIKey == "" {
		return "", fmt.Errorf("llmclient: API key not configured")
	}

	var body []byte
	var err error

	if c.isAnthropic() {
		body, err = c.buildAnthropicRequest(system, messages)
	} else {
		body, err = c.buildOpenAIRequest(system, messages)
	}
	if err != nil {
		return "", fmt.Errorf("llmclient: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("llmclient: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if c.isAnthropic() {
		req.Header.Set("x-api-key", c.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llmclient: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llmclient: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		slog.Warn("llmclient: API error", "status", resp.StatusCode, "body", string(respBody[:min(len(respBody), 200)]))
		return "", fmt.Errorf("llmclient: API returned status %d", resp.StatusCode)
	}

	if c.isAnthropic() {
		return c.parseAnthropicResponse(respBody)
	}
	return c.parseOpenAIResponse(respBody)
}

// --- Anthropic format ---

func (c *Client) buildAnthropicRequest(system string, messages []Message) ([]byte, error) {
	req := map[string]any{
		"model":      c.cfg.Model,
		"max_tokens": c.cfg.MaxTokens,
		"messages":   messages,
	}
	if system != "" {
		req["system"] = system
	}
	if c.cfg.Temperature != nil {
		req["temperature"] = *c.cfg.Temperature
	}
	return json.Marshal(req)
}

func (c *Client) parseAnthropicResponse(body []byte) (string, error) {
	var resp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("llmclient: parse anthropic response: %w", err)
	}
	if len(resp.Content) == 0 {
		return "", fmt.Errorf("llmclient: empty anthropic response")
	}
	return resp.Content[0].Text, nil
}

// --- OpenAI format (also works for DashScope, etc.) ---

func (c *Client) buildOpenAIRequest(system string, messages []Message) ([]byte, error) {
	msgs := make([]map[string]string, 0, len(messages)+1)
	if system != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": system})
	}
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}
	req := map[string]any{
		"model":      c.cfg.Model,
		"max_tokens": c.cfg.MaxTokens,
		"messages":   msgs,
	}
	if c.cfg.Temperature != nil {
		req["temperature"] = *c.cfg.Temperature
	}
	return json.Marshal(req)
}

func (c *Client) parseOpenAIResponse(body []byte) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("llmclient: parse openai response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llmclient: empty openai response")
	}
	return resp.Choices[0].Message.Content, nil
}

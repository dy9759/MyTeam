package agent_runner

import "context"

// Config holds per-agent LLM configuration passed to Run.
type Config struct {
	Kernel       string // "openai_compat" or "anthropic"
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
}

// AgentRunner is the interface that wraps the LLM call used by AutoReplyService.
// The real implementation calls the configured LLM endpoint; tests use a fake.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, cfg Config) (string, error)
}

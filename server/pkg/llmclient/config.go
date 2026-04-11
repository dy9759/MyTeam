package llmclient

import "os"

const (
	DefaultAnthropicEndpoint = "https://api.anthropic.com/v1/messages"
	DefaultAnthropicModel    = "claude-sonnet-4-20250514"
	DefaultDashScopeEndpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
	DefaultDashScopeModel    = "qwen-plus"
)

// FromEnv creates a Config from environment variables.
// Priority: ANTHROPIC_API_KEY > LLM_API_KEY; LLM_ENDPOINT > default; LLM_MODEL > default.
func FromEnv() Config {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("LLM_API_KEY")
	}

	endpoint := os.Getenv("LLM_ENDPOINT")
	if endpoint == "" {
		endpoint = DefaultAnthropicEndpoint
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = DefaultAnthropicModel
	}

	return Config{
		Endpoint:  endpoint,
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 1024,
	}
}

// DashScope creates a Config for the DashScope/Bailian API.
func DashScope(apiKey string) Config {
	model := os.Getenv("DASHSCOPE_MODEL")
	if model == "" {
		model = DefaultDashScopeModel
	}
	return Config{
		Endpoint:  DefaultDashScopeEndpoint,
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 2048,
	}
}

// DashScopeFromEnv creates a DashScope Config using env vars.
func DashScopeFromEnv() Config {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	return DashScope(apiKey)
}

// FromAgentConfig creates a Config from per-agent auto-reply configuration,
// falling back to env vars for any missing values.
func FromAgentConfig(endpoint, apiKey, model string) Config {
	base := FromEnv()
	if endpoint != "" {
		base.Endpoint = endpoint
	}
	if apiKey != "" {
		base.APIKey = apiKey
	}
	if model != "" {
		base.Model = model
	}
	return base
}

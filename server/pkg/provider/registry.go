// Package provider declares the static set of execution providers the
// platform knows about. Adding a provider requires a code change because
// every provider needs a corresponding Backend implementation and Daemon
// detection logic.
package provider

import "fmt"

type Kind string

const (
	KindLocalCLI Kind = "local_cli"
	KindCloudAPI Kind = "cloud_api"
)

type Spec struct {
	Key             string   `json:"key"`
	DisplayName     string   `json:"display_name"`
	Kind            Kind     `json:"kind"`
	Executable      string   `json:"executable,omitempty"`
	SupportedModels []string `json:"supported_models,omitempty"`
	DefaultModel    string   `json:"default_model,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

var registry = map[string]Spec{
	"claude": {
		Key:             "claude",
		DisplayName:     "Claude Code",
		Kind:            KindLocalCLI,
		Executable:      "claude",
		SupportedModels: []string{"claude-opus-4-7", "claude-sonnet-4-6", "claude-haiku-4-5"},
		DefaultModel:    "claude-sonnet-4-6",
		Capabilities:    []string{"code", "tools", "mcp"},
	},
	"codex": {
		Key:             "codex",
		DisplayName:     "Codex",
		Kind:            KindLocalCLI,
		Executable:      "codex",
		SupportedModels: []string{"gpt-5.4"},
		DefaultModel:    "gpt-5.4",
		Capabilities:    []string{"code", "tools"},
	},
	"opencode": {
		Key:          "opencode",
		DisplayName:  "OpenCode",
		Kind:         KindLocalCLI,
		Executable:   "opencode",
		Capabilities: []string{"code"},
	},
	"cloud_llm": {
		Key:          "cloud_llm",
		DisplayName:  "Cloud LLM",
		Kind:         KindCloudAPI,
		Capabilities: []string{"chat", "tools"},
	},
}

// List returns all registered providers in deterministic order.
func List() []Spec {
	keys := []string{"claude", "codex", "opencode", "cloud_llm"}
	out := make([]Spec, 0, len(keys))
	for _, k := range keys {
		out = append(out, registry[k])
	}
	return out
}

// Get returns a single provider spec by key.
func Get(key string) (Spec, bool) {
	s, ok := registry[key]
	return s, ok
}

// Validate returns an error if the key is not a registered provider.
func Validate(key string) error {
	if _, ok := registry[key]; !ok {
		return fmt.Errorf("provider %q is not registered", key)
	}
	return nil
}

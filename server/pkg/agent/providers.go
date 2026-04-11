package agent

import "sort"

// ProviderDefinition describes a supported coding-agent provider.
type ProviderDefinition struct {
	ID                     string
	DisplayName            string
	DefaultExecutable      string
	ExecutableEnvVar       string
	LegacyExecutableEnvVar string
	DefaultModelEnv        string
	LegacyModelEnv         string
	InstructionFile        string
	Capabilities           []string
	InstallHint            string
}

var providerDefinitions = map[string]ProviderDefinition{
	"claude": {
		ID:                     "claude",
		DisplayName:            "Claude Code",
		DefaultExecutable:      "claude",
		ExecutableEnvVar:       "MYTEAM_CLAUDE_PATH",
		LegacyExecutableEnvVar: "",
		DefaultModelEnv:        "MYTEAM_CLAUDE_MODEL",
		LegacyModelEnv:         "",
		InstructionFile:        "CLAUDE.md",
		Capabilities: []string{
			"skills",
			"session_resumption",
			"streaming",
			"local_runtime",
		},
		InstallHint: "Install Claude Code CLI and ensure `claude` is on PATH.",
	},
	"codex": {
		ID:                     "codex",
		DisplayName:            "Codex",
		DefaultExecutable:      "codex",
		ExecutableEnvVar:       "MYTEAM_CODEX_PATH",
		LegacyExecutableEnvVar: "",
		DefaultModelEnv:        "MYTEAM_CODEX_MODEL",
		LegacyModelEnv:         "",
		InstructionFile:        "AGENTS.md",
		Capabilities: []string{
			"skills",
			"workspace_tools",
			"session_resumption",
			"streaming",
			"local_runtime",
		},
		InstallHint: "Install Codex CLI and ensure `codex` is on PATH.",
	},
	"opencode": {
		ID:                     "opencode",
		DisplayName:            "OpenCode",
		DefaultExecutable:      "opencode",
		ExecutableEnvVar:       "MYTEAM_OPENCODE_PATH",
		LegacyExecutableEnvVar: "",
		DefaultModelEnv:        "MYTEAM_OPENCODE_MODEL",
		LegacyModelEnv:         "",
		InstructionFile:        "AGENTS.md",
		Capabilities: []string{
			"skills",
			"json_streaming",
			"local_runtime",
		},
		InstallHint: "Install OpenCode CLI and ensure `opencode` is on PATH.",
	},
}

// RegisteredProviders returns all supported providers sorted by ID.
func RegisteredProviders() []ProviderDefinition {
	ids := make([]string, 0, len(providerDefinitions))
	for id := range providerDefinitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ProviderDefinition, 0, len(ids))
	for _, id := range ids {
		out = append(out, providerDefinitions[id])
	}
	return out
}

// LookupProvider returns metadata for a single provider.
func LookupProvider(id string) (ProviderDefinition, bool) {
	def, ok := providerDefinitions[id]
	return def, ok
}

// SupportedTypes returns the registered provider IDs.
func SupportedTypes() []string {
	defs := RegisteredProviders()
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.ID)
	}
	return out
}

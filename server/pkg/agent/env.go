package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// nestedClaudeVars are environment variables set by an enclosing Claude Code
// process. If a daemon-spawned agent inherits them, a nested Claude CLI may
// detect the parent session and either refuse to start or reuse the wrong
// session state. Strip them unconditionally before spawning any child agent.
var nestedClaudeVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
}

// pathSupplements lists directories commonly holding user-installed CLI tools
// (npm globals, pipx, Homebrew). GUI-launched processes and systemd services
// often inherit a minimal PATH that omits these, so `claude`, `codex`, or
// `opencode` installed via `npm i -g` or Homebrew can't be resolved.
// Any directories already in PATH are not re-added.
func pathSupplements() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	dirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
	}
	if home != "" {
		dirs = append([]string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".bun", "bin"),
		}, dirs...)
	}
	return dirs
}

// buildEnv returns the child-process environment derived from the current
// process environment, with nested-agent markers stripped, PATH supplemented
// with common tool directories, and the caller's `extra` overrides applied
// last so they win on conflict.
func buildEnv(extra map[string]string) []string {
	env := os.Environ()
	env = stripNestedClaudeVars(env)
	env = supplementPath(env, pathSupplements())
	for k, v := range extra {
		env = setEnv(env, k, v)
	}
	return env
}

// stripNestedClaudeVars removes CLAUDECODE / CLAUDE_CODE_ENTRYPOINT entries.
func stripNestedClaudeVars(env []string) []string {
	out := env[:0]
	for _, kv := range env {
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key := kv[:idx]
			if containsString(nestedClaudeVars, key) {
				continue
			}
		}
		out = append(out, kv)
	}
	return out
}

// supplementPath appends any supplement directories that are not already in
// PATH to the end of PATH. Returns env unchanged if supplements is empty.
func supplementPath(env []string, supplements []string) []string {
	if len(supplements) == 0 {
		return env
	}
	current, idx := findEnv(env, "PATH")
	existing := splitPath(current)
	seen := make(map[string]struct{}, len(existing))
	for _, d := range existing {
		seen[d] = struct{}{}
	}
	for _, d := range supplements {
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		existing = append(existing, d)
		seen[d] = struct{}{}
	}
	joined := strings.Join(existing, string(os.PathListSeparator))
	if idx >= 0 {
		env[idx] = "PATH=" + joined
		return env
	}
	return append(env, "PATH="+joined)
}

func findEnv(env []string, key string) (string, int) {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):], i
		}
	}
	return "", -1
}

func setEnv(env []string, key, value string) []string {
	entry := key + "=" + value
	if _, idx := findEnv(env, key); idx >= 0 {
		env[idx] = entry
		return env
	}
	return append(env, entry)
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(p, string(os.PathListSeparator))
	out := parts[:0]
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

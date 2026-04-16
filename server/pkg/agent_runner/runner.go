package agent_runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Config carries the per-invocation LLM config pulled from agent.cloud_llm_config.
type Config struct {
	Kernel       string // "openai_compat" (default) or "anthropic"
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
}

// AgentRunner is the interface consumed by AutoReplyService.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, cfg Config) (string, error)
}

// Runner spawns the Python claude-agent-sdk child process.
type Runner struct {
	PythonPath string
	ScriptPath string
	Timeout    time.Duration
}

// NewRunner returns a Runner with defaults resolved from env vars and the source location.
func NewRunner() *Runner {
	scriptPath := os.Getenv("AGENT_RUNNER_SCRIPT_PATH")
	if scriptPath == "" {
		_, thisFile, _, _ := runtime.Caller(0)
		scriptPath = filepath.Join(filepath.Dir(thisFile), "claude_reply.py")
	}
	python := os.Getenv("AGENT_RUNNER_PYTHON")
	if python == "" {
		python = "python3"
	}
	return &Runner{
		PythonPath: python,
		ScriptPath: scriptPath,
		Timeout:    60 * time.Second,
	}
}

// Event is the NDJSON schema the Python side emits on stdout.
type Event struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

// Run sends prompt to the Python subprocess and returns the reply.
func (r *Runner) Run(ctx context.Context, prompt string, cfg Config) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, r.PythonPath, r.ScriptPath)
	env := os.Environ()
	switch cfg.Kernel {
	case "anthropic":
		env = append(env,
			"ANTHROPIC_BASE_URL="+cfg.BaseURL,
			"ANTHROPIC_API_KEY="+cfg.APIKey,
			"ANTHROPIC_MODEL="+cfg.Model,
		)
	default:
		env = append(env,
			"OPENAI_BASE_URL="+cfg.BaseURL,
			"OPENAI_API_KEY="+cfg.APIKey,
			"OPENAI_MODEL="+cfg.Model,
		)
	}
	if cfg.SystemPrompt != "" {
		env = append(env, "AGENT_SYSTEM_PROMPT="+cfg.SystemPrompt)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("spawn runner: %w", err)
	}

	go func() {
		defer stdin.Close()
		_, _ = stdin.Write([]byte(prompt))
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var lastDone, lastErrMsg string
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "done":
			lastDone = ev.Text
		case "error":
			lastErrMsg = ev.Message
		}
	}

	waitErr := cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("claude runner timed out after %s", r.Timeout)
	}
	if waitErr != nil {
		if lastErrMsg != "" {
			return "", fmt.Errorf("runner failed: %s", lastErrMsg)
		}
		return "", fmt.Errorf("runner exited non-zero: %w", waitErr)
	}
	if lastDone == "" {
		return "", errors.New("runner finished with no reply text")
	}
	return lastDone, nil
}

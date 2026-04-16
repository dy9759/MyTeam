package agent_runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner returns a Runner that executes the bash fake script.
// PythonPath holds the script path; ScriptPath holds the mode arg the fake reads.
func fakeRunner(t *testing.T, timeout time.Duration) *Runner {
	t.Helper()
	script, err := filepath.Abs("testdata/fake_runner.sh")
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{
		PythonPath: script,
		ScriptPath: "success",
		Timeout:    timeout,
	}
}

func TestRun_Success(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	out, err := r.Run(context.Background(), "hello", Config{
		Kernel: "openai_compat", BaseURL: "b", APIKey: "k", Model: "m",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "echo: hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRun_EmitsError(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "error"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing sdk") {
		t.Fatalf("error should mention sdk: %v", err)
	}
}

func TestRun_NoDoneEvent(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "nodone"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "no reply text") {
		t.Fatalf("expected 'no reply text' error, got: %v", err)
	}
}

func TestRun_Timeout(t *testing.T) {
	r := fakeRunner(t, 200*time.Millisecond)
	r.ScriptPath = "timeout"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestRun_EnvInjectionOpenAI(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "env_openai"
	out, err := r.Run(context.Background(), "x", Config{
		Kernel: "openai_compat", BaseURL: "http://b", APIKey: "sk-123", Model: "qwen-plus",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "http://b|sk-123|qwen-plus" {
		t.Fatalf("env not injected: %q", out)
	}
}

func TestRun_EnvInjectionAnthropic(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "env_anthropic"
	out, err := r.Run(context.Background(), "x", Config{
		Kernel: "anthropic", BaseURL: "http://a", APIKey: "sk-ant", Model: "sonnet",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "http://a|sk-ant|sonnet" {
		t.Fatalf("anthropic env not injected: %q", out)
	}
}

func TestRun_NoisyStdout(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "noisy"
	out, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected 'ok', got %q", out)
	}
}

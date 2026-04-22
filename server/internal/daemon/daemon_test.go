package daemon

import (
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestSelectBackendType(t *testing.T) {
	t.Parallel()

	task := Task{AgentID: "agent-1", IssueID: "issue-42"}

	tests := []struct {
		name        string
		provider    string
		claudeMode  string
		wantBackend string
		wantKey     string
	}{
		{"claude single mode returns default backend", "claude", ClaudeModeSingle, "claude", ""},
		{"claude persistent mode returns pooled backend", "claude", ClaudeModePersistent, "claude-persistent", "agent-1:issue-42"},
		{"codex ignores claude mode", "codex", ClaudeModePersistent, "codex", ""},
		{"opencode ignores claude mode", "opencode", ClaudeModePersistent, "opencode", ""},
		{"empty mode treated as single", "claude", "", "claude", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{cfg: Config{ClaudeMode: tt.claudeMode}}
			backend, key := d.selectBackendType(tt.provider, task)
			if backend != tt.wantBackend {
				t.Errorf("backend: got %q, want %q", backend, tt.wantBackend)
			}
			if key != tt.wantKey {
				t.Errorf("session key: got %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"myteam issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

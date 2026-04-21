package agent

import (
	"os"
	"strings"
	"testing"
)

func TestStripNestedClaudeVars(t *testing.T) {
	t.Parallel()

	env := []string{
		"PATH=/usr/bin",
		"CLAUDECODE=1",
		"HOME=/home/test",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"USER=alice",
	}

	got := stripNestedClaudeVars(env)

	for _, kv := range got {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_ENTRYPOINT=") {
			t.Errorf("expected %q to be removed, but it was retained", kv)
		}
	}

	wantSurvivors := []string{"PATH=/usr/bin", "HOME=/home/test", "USER=alice"}
	for _, want := range wantSurvivors {
		if !containsString(got, want) {
			t.Errorf("expected to retain %q, missing from %v", want, got)
		}
	}
}

func TestStripNestedClaudeVarsOnlyMatchesFullKey(t *testing.T) {
	t.Parallel()

	// CLAUDECODE_FOO is NOT in the strip list; only exact key matches count.
	env := []string{
		"CLAUDECODE=1",
		"CLAUDECODE_EXTRA=keep",
	}
	got := stripNestedClaudeVars(env)

	if containsString(got, "CLAUDECODE=1") {
		t.Error("CLAUDECODE= should have been stripped")
	}
	if !containsString(got, "CLAUDECODE_EXTRA=keep") {
		t.Error("CLAUDECODE_EXTRA= should not have been stripped")
	}
}

func TestSupplementPathAppendsMissingDirs(t *testing.T) {
	t.Parallel()

	sep := string(os.PathListSeparator)
	env := []string{"PATH=/usr/bin" + sep + "/bin"}
	supplements := []string{"/opt/homebrew/bin", "/usr/local/bin"}

	got := supplementPath(env, supplements)

	path, idx := findEnv(got, "PATH")
	if idx < 0 {
		t.Fatal("PATH missing after supplementPath")
	}
	for _, s := range supplements {
		if !strings.Contains(path, s) {
			t.Errorf("PATH missing supplement %q; got %q", s, path)
		}
	}
	// Existing entries preserved.
	if !strings.Contains(path, "/usr/bin") || !strings.Contains(path, "/bin") {
		t.Errorf("existing PATH entries lost; got %q", path)
	}
}

func TestSupplementPathDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	sep := string(os.PathListSeparator)
	env := []string{"PATH=/opt/homebrew/bin" + sep + "/usr/bin"}
	got := supplementPath(env, []string{"/opt/homebrew/bin"})

	path, _ := findEnv(got, "PATH")
	if strings.Count(path, "/opt/homebrew/bin") != 1 {
		t.Errorf("expected /opt/homebrew/bin to appear once, got %q", path)
	}
}

func TestSupplementPathAddsPathWhenMissing(t *testing.T) {
	t.Parallel()

	env := []string{"HOME=/home/test"}
	got := supplementPath(env, []string{"/opt/homebrew/bin"})

	path, idx := findEnv(got, "PATH")
	if idx < 0 {
		t.Fatal("expected PATH to be added when missing")
	}
	if path != "/opt/homebrew/bin" {
		t.Errorf("PATH: got %q, want %q", path, "/opt/homebrew/bin")
	}
}

func TestBuildEnvAppliesExtrasAsOverrides(t *testing.T) {
	t.Setenv("FOO_TEST_KEY", "original")

	got := buildEnv(map[string]string{"FOO_TEST_KEY": "overridden"})

	val, idx := findEnv(got, "FOO_TEST_KEY")
	if idx < 0 {
		t.Fatal("FOO_TEST_KEY missing from env")
	}
	if val != "overridden" {
		t.Errorf("FOO_TEST_KEY: got %q, want %q", val, "overridden")
	}
	// Must appear exactly once.
	count := 0
	for _, kv := range got {
		if strings.HasPrefix(kv, "FOO_TEST_KEY=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("FOO_TEST_KEY occurrences: got %d, want 1", count)
	}
}

func TestBuildEnvStripsNestedClaudeVars(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")

	got := buildEnv(nil)

	for _, kv := range got {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_ENTRYPOINT=") {
			t.Errorf("buildEnv leaked nested claude var %q", kv)
		}
	}
}

func TestBuildEnvSupplementsPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")

	got := buildEnv(nil)

	path, idx := findEnv(got, "PATH")
	if idx < 0 {
		t.Fatal("PATH missing from buildEnv output")
	}
	// At least one supplement from pathSupplements() must be appended.
	found := false
	for _, s := range pathSupplements() {
		if strings.Contains(path, s) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PATH missing any supplement entries; got %q", path)
	}
}

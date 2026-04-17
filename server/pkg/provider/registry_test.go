package provider

import (
	"testing"
)

func TestRegistryHasFourProviders(t *testing.T) {
	all := List()
	if len(all) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(all))
	}
	want := map[string]bool{"claude": false, "codex": false, "opencode": false, "cloud_llm": false}
	for _, p := range all {
		want[p.Key] = true
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing provider %q", k)
		}
	}
}

func TestGetReturnsSpec(t *testing.T) {
	spec, ok := Get("claude")
	if !ok {
		t.Fatal("expected claude to exist")
	}
	if spec.Kind != KindLocalCLI {
		t.Errorf("expected LocalCLI, got %v", spec.Kind)
	}
	if spec.Executable != "claude" {
		t.Errorf("expected executable 'claude', got %q", spec.Executable)
	}
}

func TestGetReturnsFalseForUnknown(t *testing.T) {
	if _, ok := Get("not-a-provider"); ok {
		t.Fatal("expected unknown provider to return ok=false")
	}
}

func TestValidateAcceptsKnownProvider(t *testing.T) {
	if err := Validate("codex"); err != nil {
		t.Errorf("expected codex to validate, got %v", err)
	}
}

func TestValidateRejectsUnknownProvider(t *testing.T) {
	if err := Validate("legacy_local"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestCloudLLMHasNoExecutable(t *testing.T) {
	spec, _ := Get("cloud_llm")
	if spec.Kind != KindCloudAPI {
		t.Errorf("expected CloudAPI kind, got %v", spec.Kind)
	}
	if spec.Executable != "" {
		t.Errorf("expected empty executable, got %q", spec.Executable)
	}
}

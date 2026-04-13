package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadCLIConfigForProfileReadsMyTeamConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	configPath := filepath.Join(home, ".multica", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	want := CLIConfig{
		ServerURL:   "https://api.myteam.ai",
		AppURL:      "https://myteam.ai",
		WorkspaceID: "ws_123",
		Token:       "tok_123",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadCLIConfigForProfile() = %+v, want %+v", got, want)
	}
}

func TestLoadCLIConfigForProfileIgnoresUnknownConfigDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	otherConfigPath := filepath.Join(home, ".legacy-cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(otherConfigPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	legacy := CLIConfig{
		ServerURL:   "https://legacy.example.com",
		WorkspaceID: "legacy_ws",
		Token:       "legacy_token",
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(otherConfigPath, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile() error = %v", err)
	}
	if !reflect.DeepEqual(got, CLIConfig{}) {
		t.Fatalf("LoadCLIConfigForProfile() = %+v, want empty config", got)
	}
}

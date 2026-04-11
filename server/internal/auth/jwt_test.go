package auth

import (
	"strings"
	"testing"
)

func TestGeneratePATTokenUsesMyTeamPrefix(t *testing.T) {
	token, err := GeneratePATToken()
	if err != nil {
		t.Fatalf("GeneratePATToken() error = %v", err)
	}
	if !strings.HasPrefix(token, "myt_") {
		t.Fatalf("GeneratePATToken() = %q, want prefix %q", token, "myt_")
	}
}

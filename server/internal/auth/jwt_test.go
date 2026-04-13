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
	if !strings.HasPrefix(token, PersonalAccessTokenPrefix) {
		t.Fatalf("GeneratePATToken() = %q, want prefix %q", token, PersonalAccessTokenPrefix)
	}
}

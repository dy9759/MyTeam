package handler

import (
	"testing"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func TestWorkspaceToResponse_InvalidSettingsJSON(t *testing.T) {
	_, err := workspaceToResponse(db.Workspace{
		Settings: []byte(`"bad-settings"`),
		Repos:    []byte(`[]`),
	})
	if err == nil {
		t.Fatal("expected workspaceToResponse to fail for invalid settings payload shape")
	}
}

func TestWorkspaceToResponse_InvalidReposJSON(t *testing.T) {
	_, err := workspaceToResponse(db.Workspace{
		Settings: []byte(`{}`),
		Repos:    []byte(`"bad-repos"`),
	})
	if err == nil {
		t.Fatal("expected workspaceToResponse to fail for invalid repos payload shape")
	}
}

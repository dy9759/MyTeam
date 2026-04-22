package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
)

func TestCheckoutRepoRejectsCloudRuntime(t *testing.T) {
	result, err := (CheckoutRepo{}).Exec(context.Background(), nil, mcptool.Context{
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{"project_id": uuid.NewString()})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if !hasErrorCode(result, errcode.MCPToolNotAvailable.Code) {
		t.Fatalf("expected %s, got %#v", errcode.MCPToolNotAvailable.Code, result.Errors)
	}
}

func TestLocalFileReadRejectsCloudRuntime(t *testing.T) {
	result, err := (LocalFileRead{}).Exec(context.Background(), nil, mcptool.Context{
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{"path": "/tmp/example.txt"})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if !hasErrorCode(result, errcode.MCPToolNotAvailable.Code) {
		t.Fatalf("expected %s, got %#v", errcode.MCPToolNotAvailable.Code, result.Errors)
	}
}

func TestApplyPatchRejectsCloudRuntime(t *testing.T) {
	result, err := (ApplyPatch{}).Exec(context.Background(), nil, mcptool.Context{
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"project_id": uuid.NewString(),
		"patch":      "diff --git a/file.txt b/file.txt\n",
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if !hasErrorCode(result, errcode.MCPToolNotAvailable.Code) {
		t.Fatalf("expected %s, got %#v", errcode.MCPToolNotAvailable.Code, result.Errors)
	}
}

func hasErrorCode(result mcptool.Result, code string) bool {
	for _, got := range result.Errors {
		if got == code {
			return true
		}
	}
	return false
}

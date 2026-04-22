package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ApplyPatch applies a unified-diff patch to a local project working tree.
// LOCAL ONLY — refused in cloud runtime.
type ApplyPatch struct{}

func (ApplyPatch) Name() string { return "apply_patch" }

func (ApplyPatch) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "patch"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"patch":      map[string]string{"type": "string"},
			"repo_path":  map[string]string{"type": "string"},
			"workdir":    map[string]string{"type": "string"},
		},
	}
}

func (ApplyPatch) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal}
}

func (ApplyPatch) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if ws.RuntimeMode != mcptool.RuntimeLocal {
		return toolNotAvailable("apply_patch is not available in cloud runtime; TODO: wire SDK sandbox patch application"), nil
	}

	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	if _, err := loadProjectForWorkspace(ctx, q, ws, projectID); err != nil {
		if result, ok := accessErrorResult(err); ok {
			return result, nil
		}
		return mcptool.Result{}, err
	}

	patch := stringArg(args, "patch")
	if patch == "" {
		return mcptool.Result{}, fmt.Errorf("patch is required")
	}
	repoPath := stringArg(args, "repo_path")
	if repoPath == "" {
		repoPath = stringArg(args, "workdir")
	}
	if repoPath == "" {
		repoPath = "."
	}
	absRepoPath, allowed, err := allowedPath(repoPath)
	if err != nil {
		return mcptool.Result{}, err
	}
	if !allowed {
		return mcptool.Result{
			Note:   "repo_path is outside daemon allowed paths",
			Errors: []string{errcode.MCPPermissionDenied.Code},
		}, nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", absRepoPath, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = strings.NewReader(patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("git apply: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return mcptool.Result{Data: map[string]any{
		"project_id": projectID.String(),
		"repo_path":  absRepoPath,
		"applied":    true,
	}}, nil
}

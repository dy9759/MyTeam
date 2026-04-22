package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// CheckoutRepo clones or refreshes a project repo into the local daemon workspace.
// LOCAL ONLY — refused in cloud runtime.
type CheckoutRepo struct{}

func (CheckoutRepo) Name() string { return "checkout_repo" }

func (CheckoutRepo) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"repo_url":   map[string]string{"type": "string"},
			"workdir":    map[string]string{"type": "string"},
		},
	}
}

func (CheckoutRepo) RuntimeModes() []string { return []string{mcptool.RuntimeLocal} }

func (CheckoutRepo) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if ws.RuntimeMode != mcptool.RuntimeLocal {
		return mcptool.Result{
			Note:   "checkout_repo is local-only",
			Errors: []string{errcode.MCPToolNotAvailable.Code},
		}, nil
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

	repoURL, err := selectRepoURL(ctx, q, ws.WorkspaceID, args)
	if err != nil {
		return mcptool.Result{}, err
	}

	workdir := stringArg(args, "workdir")
	if workdir == "" {
		workdir = "."
	}
	absWorkdir, allowed, err := allowedPath(workdir)
	if err != nil {
		return mcptool.Result{}, err
	}
	if !allowed {
		return mcptool.Result{
			Note:   "workdir is outside daemon allowed paths",
			Errors: []string{errcode.MCPPermissionDenied.Code},
		}, nil
	}

	checkoutPath := filepath.Join(absWorkdir, repoNameFromURL(repoURL))
	if err := checkoutOrUpdateRepo(ctx, repoURL, checkoutPath); err != nil {
		return mcptool.Result{}, err
	}

	return mcptool.Result{Data: map[string]any{
		"project_id": projectID.String(),
		"repo_url":   repoURL,
		"path":       checkoutPath,
	}}, nil
}

func checkoutOrUpdateRepo(ctx context.Context, repoURL, checkoutPath string) error {
	if gitDirExists(checkoutPath) {
		cmd := exec.CommandContext(ctx, "git", "-C", checkoutPath, "fetch", "--all", "--prune")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %s: %w", strings.TrimSpace(string(output)), err)
		}
		return nil
	}

	if entries, err := os.ReadDir(checkoutPath); err == nil && len(entries) > 0 {
		return fmt.Errorf("checkout path exists and is not an empty git repo: %s", checkoutPath)
	}
	if err := os.MkdirAll(filepath.Dir(checkoutPath), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, checkoutPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func gitDirExists(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

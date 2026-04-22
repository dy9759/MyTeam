package tools

import (
	"context"
	"fmt"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// CreatePR opens a pull request on the project's source-control provider.
// No server-side implementation exists yet.
type CreatePR struct{}

func (CreatePR) Name() string { return "create_pr" }

func (CreatePR) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "branch", "title", "body"},
		"properties": map[string]any{
			"project_id":  map[string]string{"type": "string", "format": "uuid"},
			"branch":      map[string]string{"type": "string"},
			"title":       map[string]string{"type": "string"},
			"body":        map[string]string{"type": "string"},
			"base_branch": map[string]string{"type": "string"},
			"provider":    map[string]string{"type": "string"},
			"repo_url":    map[string]string{"type": "string"},
			"secret_key":  map[string]string{"type": "string"},
		},
	}
}

func (CreatePR) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (CreatePR) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	project, err := loadProjectForWorkspace(ctx, q, ws, projectID)
	if err != nil {
		if result, ok := accessErrorResult(err); ok {
			return result, nil
		}
		return mcptool.Result{}, err
	}

	branch := stringArg(args, "branch")
	title := stringArg(args, "title")
	body := stringArg(args, "body")
	if branch == "" || title == "" || body == "" {
		return mcptool.Result{}, fmt.Errorf("branch, title, and body are required")
	}

	repoURL, err := selectRepoURL(ctx, q, ws.WorkspaceID, args)
	if err != nil {
		return mcptool.Result{}, err
	}
	provider := stringArg(args, "provider")
	if provider == "" {
		provider = inferProvider(repoURL)
	}
	secretKey := stringArg(args, "secret_key")
	if secretKey == "" {
		secretKey = provider + "_token"
	}

	token, err := service.NewSecretService(q).GetPlaintext(ctx, ws.WorkspaceID, secretKey)
	if err != nil {
		return mcptool.Result{}, err
	}

	baseBranch := stringArg(args, "base_branch")
	if baseBranch == "" {
		baseBranch = "main"
	}

	return mcptool.Result{
		Stub: true,
		Note: "SCM pull request API call is stubbed; workspace secret token was loaded",
		Data: map[string]any{
			"project_id":    projectID.String(),
			"workspace_id":  uuidString(project.WorkspaceID),
			"provider":      provider,
			"repo_url":      repoURL,
			"branch":        branch,
			"base_branch":   baseBranch,
			"title":         title,
			"body":          body,
			"secret_key":    secretKey,
			"secret_loaded": token != "",
		},
	}, nil
}

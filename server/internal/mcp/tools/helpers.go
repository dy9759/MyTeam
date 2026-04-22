// Package tools: helpers.go — shared utilities for the MCP tool
// implementations. Holds argument parsing, permission checks, and the
// pgtype <-> uuid conversion helper shared by the tools.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// errMCPPermissionDenied / errArgMissing are the canonical error sentinels
// callers compare against to render the right errcode envelope.
var (
	errMCPPermissionDenied = errors.New("mcp tool permission denied")
	errArgMissing          = errors.New("required argument missing")
)

// workspaceRepo mirrors the JSONB shape stored in workspace.repos.
type workspaceRepo struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// stringArg pulls a (trimmed) string from the args map. Missing or
// non-string values return "" so callers can use the empty check directly.
func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

// uuidArg parses a required uuid argument. Missing → wraps errArgMissing.
func uuidArg(args map[string]any, key string) (uuid.UUID, error) {
	raw := stringArg(args, key)
	if raw == "" {
		return uuid.Nil, fmt.Errorf("%w: %s", errArgMissing, key)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return id, nil
}

// optionalUUIDArg parses a uuid argument when present. Missing returns
// uuid.Nil with no error; invalid still errors so we never silently drop a
// malformed FK.
func optionalUUIDArg(args map[string]any, key string) (uuid.UUID, error) {
	raw := stringArg(args, key)
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return id, nil
}

// mapArg pulls a map-typed (JSON object) arg. Missing returns nil, false.
func mapArg(args map[string]any, key string) (map[string]any, bool) {
	v, ok := args[key]
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	return m, true
}

// pgUUID converts a non-nil uuid.UUID to a valid pgtype.UUID. uuid.Nil
// produces Valid=false (NULL).
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

// toPgNullText converts an optional string to pgtype.Text; empty becomes
// Valid=false (NULL).
func toPgNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// uuidString stringifies a pgtype.UUID, returning "" for NULL.
func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

// sameUUID returns true when the pgtype value carries the given uuid.
func sameUUID(id pgtype.UUID, want uuid.UUID) bool {
	return id.Valid && uuid.UUID(id.Bytes) == want
}

// permissionDenied is the canonical "agent not allowed on this task" result.
func permissionDenied(note string) mcptool.Result {
	return mcptool.Result{
		Errors: []string{errcode.MCPPermissionDenied.Code},
		Note:   note,
	}
}

// notFoundResult returns the legacy "row missing" result for tool-specific
// kinds that do not yet have a canonical errcode constant.
func notFoundResult(kind string) mcptool.Result {
	return mcptool.Result{
		Errors: []string{kind + "_NOT_FOUND"},
		Note:   kind + " not found",
	}
}

// toolNotAvailable is the canonical "this tool isn't available in the
// current runtime mode" result (e.g. cloud calling local_file_read).
func toolNotAvailable(note string) mcptool.Result {
	return mcptool.Result{
		Note:   note,
		Errors: []string{errcode.MCPToolNotAvailable.Code},
	}
}

// accessErrorResult maps a workspace-access lookup error to a canonical
// MCP result, returning ok=false when the error doesn't match a known
// pattern (so callers can wrap it themselves).
func accessErrorResult(err error) (mcptool.Result, bool) {
	switch {
	case errors.Is(err, errMCPPermissionDenied):
		return mcptool.Result{
			Note:   "workspace permission denied",
			Errors: []string{errcode.MCPPermissionDenied.Code},
		}, true
	case errors.Is(err, pgx.ErrNoRows):
		return mcptool.Result{
			Note:   "project not found",
			Errors: []string{errcode.ProjectNotFound.Code},
		}, true
	default:
		return mcptool.Result{}, false
	}
}

// ensureWorkspaceMember verifies the caller is a member of the workspace
// (and, when an agent id is set, that the agent belongs to that workspace).
// Returns errMCPPermissionDenied for unauthorized callers.
func ensureWorkspaceMember(ctx context.Context, q *db.Queries, ws mcptool.Context) error {
	if q == nil {
		return fmt.Errorf("mcp tool: queries required")
	}
	if ws.WorkspaceID == uuid.Nil || ws.UserID == uuid.Nil {
		return errMCPPermissionDenied
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      pgUUID(ws.UserID),
		WorkspaceID: pgUUID(ws.WorkspaceID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errMCPPermissionDenied
		}
		return err
	}
	if ws.AgentID != uuid.Nil {
		if _, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          pgUUID(ws.AgentID),
			WorkspaceID: pgUUID(ws.WorkspaceID),
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errMCPPermissionDenied
			}
			return err
		}
	}
	return nil
}

// loadProjectForWorkspace combines the membership check with a project
// lookup that enforces workspace alignment.
func loadProjectForWorkspace(ctx context.Context, q *db.Queries, ws mcptool.Context, projectID uuid.UUID) (db.Project, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		return db.Project{}, err
	}
	project, err := q.GetProject(ctx, pgUUID(projectID))
	if err != nil {
		return db.Project{}, err
	}
	if !sameUUID(project.WorkspaceID, ws.WorkspaceID) {
		return db.Project{}, errMCPPermissionDenied
	}
	return project, nil
}

// ensureAgentOnTask enforces cross-cutting PRD §7.2: an agent may act on
// a task only when it is the task's actual_agent_id OR primary_assignee_id.
// Workspace membership is checked too. When ws.AgentID is uuid.Nil (human
// caller) the per-agent check is skipped — those calls are gated by
// ensureWorkspaceMember at the dispatcher layer.
func ensureAgentOnTask(ctx context.Context, q *db.Queries, ws mcptool.Context, taskID uuid.UUID) (db.Task, mcptool.Result, error) {
	task, err := q.GetTask(ctx, pgUUID(taskID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Task{}, notFoundResult("TASK"), nil
		}
		return db.Task{}, mcptool.Result{}, fmt.Errorf("get task: %w", err)
	}

	if task.WorkspaceID.Valid && task.WorkspaceID.Bytes != ws.WorkspaceID {
		return db.Task{}, permissionDenied("task does not belong to caller workspace"), nil
	}

	if ws.AgentID == uuid.Nil {
		return task, mcptool.Result{}, nil
	}

	agentBytes := ws.AgentID
	if task.ActualAgentID.Valid && task.ActualAgentID.Bytes == agentBytes {
		return task, mcptool.Result{}, nil
	}
	if task.PrimaryAssigneeID.Valid && task.PrimaryAssigneeID.Bytes == agentBytes {
		return task, mcptool.Result{}, nil
	}
	return db.Task{}, permissionDenied("agent is not actual_agent_id or primary_assignee_id of task"), nil
}

// listWorkspaceRepos parses the workspace.repos JSONB into typed structs.
func listWorkspaceRepos(ctx context.Context, q *db.Queries, workspaceID uuid.UUID) ([]workspaceRepo, error) {
	ws, err := q.GetWorkspace(ctx, pgUUID(workspaceID))
	if err != nil {
		return nil, err
	}
	if len(ws.Repos) == 0 {
		return nil, nil
	}
	var repos []workspaceRepo
	if err := json.Unmarshal(ws.Repos, &repos); err != nil {
		return nil, fmt.Errorf("decode workspace repos: %w", err)
	}
	return repos, nil
}

// selectRepoURL picks a repo URL from args (explicit) or from the workspace
// configuration (first non-empty entry).
func selectRepoURL(ctx context.Context, q *db.Queries, workspaceID uuid.UUID, args map[string]any) (string, error) {
	if repoURL := stringArg(args, "repo_url"); repoURL != "" {
		return repoURL, nil
	}
	repos, err := listWorkspaceRepos(ctx, q, workspaceID)
	if err != nil {
		return "", err
	}
	for _, repo := range repos {
		if strings.TrimSpace(repo.URL) != "" {
			return strings.TrimSpace(repo.URL), nil
		}
	}
	return "", fmt.Errorf("workspace has no configured repository")
}

func inferProvider(repoURL string) string {
	lower := strings.ToLower(repoURL)
	switch {
	case strings.Contains(lower, "gitlab"):
		return "gitlab"
	default:
		return "github"
	}
}

func repoNameFromURL(repoURL string) string {
	repoURL = strings.TrimRight(strings.TrimSpace(repoURL), "/")
	repoURL = strings.TrimSuffix(repoURL, ".git")
	if i := strings.LastIndex(repoURL, "/"); i >= 0 {
		repoURL = repoURL[i+1:]
	}
	if i := strings.LastIndex(repoURL, ":"); i >= 0 {
		repoURL = repoURL[i+1:]
	}
	if repoURL == "" {
		return "repo"
	}
	return repoURL
}

// allowedPath checks the given path is within an allowed root and returns
// the absolute, symlink-resolved version.
func allowedPath(path string) (string, bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}

	roots, err := allowedPathRoots()
	if err != nil {
		return "", false, err
	}
	for _, root := range roots {
		if pathWithinRoot(absPath, root) {
			return absPath, true, nil
		}
	}
	return absPath, false, nil
}

func allowedPathRoots() ([]string, error) {
	var roots []string
	for _, env := range []string{"MYTEAM_DAEMON_ALLOWED_PATHS", "MYTEAM_ALLOWED_PATHS"} {
		for _, root := range filepath.SplitList(os.Getenv(env)) {
			if strings.TrimSpace(root) != "" {
				roots = append(roots, root)
			}
		}
	}
	for _, env := range []string{"MYTEAM_WORKDIR", "MYTEAM_WORKSPACES_ROOT"} {
		if root := strings.TrimSpace(os.Getenv(env)); root != "" {
			roots = append(roots, root)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}

	seen := map[string]bool{}
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if realRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
			absRoot = realRoot
		}
		absRoot = filepath.Clean(absRoot)
		if !seen[absRoot] {
			normalized = append(normalized, absRoot)
			seen[absRoot] = true
		}
	}
	return normalized, nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

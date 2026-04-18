package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CreateComment creates a new comment on an issue.
type CreateComment struct{}

func (CreateComment) Name() string { return "create_comment" }

func (CreateComment) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "body"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
			"body":     map[string]string{"type": "string"},
		},
	}
}

func (CreateComment) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (CreateComment) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := mcptool.RequireMember(ctx, ws); err != nil {
		return mcptool.Result{}, err
	}
	issueID, err := requireUUIDArg(args, "issue_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	body, _ := args["body"].(string)
	if body == "" {
		return mcptool.Result{}, errors.New("body is required")
	}
	if ws.UserID == uuid.Nil {
		return mcptool.Result{}, errors.New("user_id required")
	}

	// Verify the issue belongs to this workspace; this is the workspace
	// boundary check per cross-cutting PRD §7.1.
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          uuidToPgtype(issueID),
		WorkspaceID: uuidToPgtype(ws.WorkspaceID),
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("issue not found: %w", err)
	}

	// Author identity follows the same rule as the HTTP handler: when the
	// caller is acting on behalf of an agent (AgentID set), record the
	// agent as author; otherwise the user is the author.
	authorType := "member"
	authorID := ws.UserID
	if ws.AgentID != uuid.Nil {
		authorType = "agent"
		authorID = ws.AgentID
	}

	// Route through CommentService so MCP-created comments fire the same
	// side effects as HTTP-created comments: identifier expansion, WS event,
	// on_comment trigger, and @mentioned-agent enqueue. Without this, an
	// agent that gets @-mentioned in an MCP comment would never auto-reply.
	if ws.Comments != nil {
		comment, err := ws.Comments.Create(ctx, service.CreateCommentInput{
			Issue:       issue,
			AuthorType:  authorType,
			AuthorID:    uuidToPgtype(authorID),
			Content:     body,
			CommentType: "comment",
		})
		if err != nil {
			return mcptool.Result{}, fmt.Errorf("create comment: %w", err)
		}
		return mcptool.Result{Data: commentToMap(comment)}, nil
	}

	// Fallback path for callers (mostly unit tests) that did not wire a
	// CommentService into the MCP context. Side effects are SKIPPED — log a
	// warning so we notice if a production code path forgets to wire it.
	slog.Warn("mcp create_comment: CommentService not wired; skipping side effects (mention expand, event publish, on_comment trigger, @mention enqueue)", "workspace_id", ws.WorkspaceID.String(), "issue_id", issueID.String())
	comment, err := q.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  authorType,
		AuthorID:    uuidToPgtype(authorID),
		Content:     body,
		Type:        "comment",
		ParentID:    pgtype.UUID{},
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("create comment: %w", err)
	}

	return mcptool.Result{Data: commentToMap(comment)}, nil
}

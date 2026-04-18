package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
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

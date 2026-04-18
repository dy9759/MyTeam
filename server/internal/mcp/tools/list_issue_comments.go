package tools

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListIssueComments returns paginated comments for an issue.
type ListIssueComments struct{}

func (ListIssueComments) Name() string { return "list_issue_comments" }

func (ListIssueComments) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
			"limit":    map[string]string{"type": "integer"},
			"offset":   map[string]string{"type": "integer"},
		},
	}
}

func (ListIssueComments) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ListIssueComments) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := mcptool.RequireMember(ctx, ws); err != nil {
		return mcptool.Result{}, err
	}
	issueID, err := requireUUIDArg(args, "issue_id")
	if err != nil {
		return mcptool.Result{}, err
	}

	// Verify the issue belongs to this workspace before reading comments.
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          uuidToPgtype(issueID),
		WorkspaceID: uuidToPgtype(ws.WorkspaceID),
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("issue not found: %w", err)
	}

	comments, err := q.ListComments(ctx, db.ListCommentsParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("list comments: %w", err)
	}

	limit, offset := paginationArgs(args)
	sliced := paginateComments(comments, limit, offset)

	out := make([]map[string]any, len(sliced))
	for i, c := range sliced {
		out[i] = commentToMap(c)
	}
	return mcptool.Result{Data: map[string]any{
		"comments": out,
		"total":    len(comments),
	}}, nil
}

// paginationArgs reads optional limit/offset, defaulting to no slicing
// when absent. Negative values are coerced to zero.
func paginationArgs(args map[string]any) (int, int) {
	limit := -1
	offset := 0
	if v, ok := args["limit"]; ok {
		limit = intArg(v)
		if limit < 0 {
			limit = 0
		}
	}
	if v, ok := args["offset"]; ok {
		offset = intArg(v)
		if offset < 0 {
			offset = 0
		}
	}
	return limit, offset
}

func intArg(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

func paginateComments(comments []db.Comment, limit, offset int) []db.Comment {
	if offset >= len(comments) {
		return []db.Comment{}
	}
	tail := comments[offset:]
	if limit < 0 || limit >= len(tail) {
		return tail
	}
	return tail[:limit]
}

func commentToMap(c db.Comment) map[string]any {
	out := map[string]any{
		"id":           uuidString(c.ID),
		"issue_id":     uuidString(c.IssueID),
		"workspace_id": uuidString(c.WorkspaceID),
		"author_type":  c.AuthorType,
		"author_id":    uuidString(c.AuthorID),
		"content":      c.Content,
		"type":         c.Type,
		"created_at":   timestampString(c.CreatedAt),
		"updated_at":   timestampString(c.UpdatedAt),
	}
	if c.ParentID.Valid {
		out["parent_id"] = uuidString(c.ParentID)
	}
	return out
}

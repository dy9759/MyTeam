package tools

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// SearchProjectContext returns thread_context_item rows whose body matches the
// query, scoped to threads in the project's channel. Workspace-checked.
type SearchProjectContext struct{}

func (SearchProjectContext) Name() string { return "search_project_context" }

func (SearchProjectContext) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "query"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"query":      map[string]string{"type": "string"},
		},
	}
}

func (SearchProjectContext) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (SearchProjectContext) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	query := strings.ToLower(stringArg(args, "query"))

	project, err := loadProjectForWorkspace(ctx, q, ws, projectID)
	if err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}

	type itemOut struct {
		ID       string `json:"id"`
		ThreadID string `json:"thread_id"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		ItemType string `json:"item_type"`
	}
	items := []itemOut{}

	if !project.ChannelID.Valid {
		return mcptool.Result{Data: map[string]any{"items": items}}, nil
	}

	threads, err := q.ListThreadsByChannel(ctx, db.ListThreadsByChannelParams{
		ChannelID:   project.ChannelID,
		Status:      pgtype.Text{},
		OffsetCount: 0,
		LimitCount:  200,
	})
	if err != nil {
		return mcptool.Result{}, err
	}

	for _, t := range threads {
		ctxItems, err := q.ListThreadContextItems(ctx, t.ID)
		if err != nil {
			return mcptool.Result{}, err
		}
		for _, ci := range ctxItems {
			if query != "" && !strings.Contains(strings.ToLower(ci.Body.String), query) {
				continue
			}
			items = append(items, itemOut{
				ID:       uuidString(ci.ID),
				ThreadID: uuidString(ci.ThreadID),
				Title:    ci.Title.String,
				Body:     ci.Body.String,
				ItemType: ci.ItemType,
			})
		}
	}

	return mcptool.Result{Data: map[string]any{"items": items}}, nil
}

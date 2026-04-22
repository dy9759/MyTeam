package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func TestRequestApproval_HappyPath(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	res, err := RequestApproval{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id": pgxToUUID(t, env.TaskID).String(),
		"slot_id": uuid.Nil.String(),
		"context": "needs human go/no-go before deploy",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v (note=%s)", res.Errors, res.Note)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res.Data)
	}
	if data["recipient_id"] != pgxToUUID(t, env.OwnerID).String() {
		t.Errorf("recipient_id: want owner %s, got %v", pgxToUUID(t, env.OwnerID).String(), data["recipient_id"])
	}
	inboxIDStr, _ := data["inbox_item_id"].(string)
	if _, err := uuid.Parse(inboxIDStr); err != nil {
		t.Errorf("inbox_item_id is not a valid uuid: %q", inboxIDStr)
	}

	// The row should be retrievable via the standard list query.
	items, err := q.ListInboxUnresolved(ctx, db.ListInboxUnresolvedParams{
		RecipientID: env.OwnerID,
		LimitCount:  10,
		OffsetCount: 0,
	})
	if err != nil {
		t.Fatalf("ListInboxUnresolved: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item, got %d", len(items))
	}
	if items[0].Type != "human_input_needed" {
		t.Errorf("type: want human_input_needed, got %s", items[0].Type)
	}
	if items[0].Severity != "action_required" {
		t.Errorf("severity: want action_required, got %s", items[0].Severity)
	}
}

func TestRequestApprovalRejectsForeignWorkspaceProject(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	otherWorkspace, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "Foreign workspace " + t.Name(),
		Slug:        "foreign-" + uniqSuffix(t),
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: "FW",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	otherUser, err := q.CreateUser(ctx, db.CreateUserParams{
		Name:      "Foreign owner " + t.Name(),
		Email:     "foreign-owner+" + uniqSuffix(t) + "@example.com",
		AvatarUrl: pgtype.Text{},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	otherProject, err := q.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID:         otherWorkspace.ID,
		Title:               "Foreign project",
		Description:         pgtype.Text{String: "outside the caller workspace", Valid: true},
		Status:              "running",
		ScheduleType:        "one_time",
		CronExpr:            pgtype.Text{},
		SourceConversations: []byte(`[]`),
		ChannelID:           pgtype.UUID{},
		CreatorOwnerID:      otherUser.ID,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	pool := openTestPool(t)
	if _, err := pool.Exec(ctx, `UPDATE project_run SET project_id = $1 WHERE id = $2`, otherProject.ID, env.RunID); err != nil {
		t.Fatalf("update project_run: %v", err)
	}

	res, err := RequestApproval{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id": pgxToUUID(t, env.TaskID).String(),
		"slot_id": uuid.Nil.String(),
		"context": "should not create an inbox item",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !containsError(res.Errors, errcode.MCPPermissionDenied.Code) {
		t.Fatalf("expected MCP_PERMISSION_DENIED, got errors=%v note=%s", res.Errors, res.Note)
	}
}

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/errcode"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUploadArtifact_Headless(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	res, err := UploadArtifact{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id":       pgxToUUID(t, env.TaskID).String(),
		"slot_id":       uuid.Nil.String(),
		"execution_id":  uuid.Nil.String(),
		"artifact_type": "report",
		"title":         "Headless title",
		"summary":       "summary",
		"content":       map[string]any{"output": "ok"},
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
	if data["artifact_type"] != "report" {
		t.Errorf("artifact_type: want report, got %v", data["artifact_type"])
	}
	if _, ok := data["id"]; !ok {
		t.Error("missing id in response")
	}
	if data["file_index_id"] != nil {
		t.Errorf("headless artifact should not have file_index_id, got %v", data["file_index_id"])
	}
}

func TestUploadArtifact_FileBacked(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	// Insert a file_index row so CreateWithFile has something to point at.
	scopeJSON, _ := json.Marshal(map[string]any{"scope": "project"})
	fi, err := q.CreateFileIndex(ctx, db.CreateFileIndexParams{
		WorkspaceID:          env.WorkspaceID,
		UploaderIdentityID:   env.OwnerID,
		UploaderIdentityType: "member",
		OwnerID:              env.OwnerID,
		SourceType:           "project",
		SourceID:             env.ProjectID,
		FileName:             "design.pdf",
		FileSize:             pgtype.Int8{Int64: 2048, Valid: true},
		ContentType:          pgtype.Text{String: "application/pdf", Valid: true},
		StoragePath:          pgtype.Text{String: "/tmp/design.pdf", Valid: true},
		AccessScope:          scopeJSON,
		ChannelID:            pgtype.UUID{},
		ProjectID:            env.ProjectID,
	})
	if err != nil {
		t.Fatalf("create file_index: %v", err)
	}

	res, err := UploadArtifact{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id":       pgxToUUID(t, env.TaskID).String(),
		"slot_id":       uuid.Nil.String(),
		"execution_id":  uuid.Nil.String(),
		"artifact_type": "design",
		"title":         "File-backed",
		"summary":       "design v1",
		"file":          pgxToUUID(t, fi.ID).String(),
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
	if data["artifact_type"] != "design" {
		t.Errorf("artifact_type: want design, got %v", data["artifact_type"])
	}
	if data["file_index_id"] != pgxToUUID(t, fi.ID).String() {
		t.Errorf("file_index_id: want %s, got %v", pgxToUUID(t, fi.ID).String(), data["file_index_id"])
	}
}

func TestUploadArtifact_DeniesUnrelatedAgent(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	// A different agent that is neither actual_agent_id nor primary_assignee_id
	// of the task. Different owner because migration 062 added a
	// (workspace_id, owner_id, agent_type='personal_agent') unique constraint.
	otherUser, err := q.CreateUser(ctx, db.CreateUserParams{
		Name:  "Outsider " + t.Name(),
		Email: "outsider+" + uniqSuffix(t) + "@example.com",
	})
	if err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	otherAgent, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: env.WorkspaceID,
		Name:        "Outsider Agent",
		Description: "should not have access",
		RuntimeID:   env.RuntimeID,
		OwnerID:     otherUser.ID,
	})
	if err != nil {
		t.Fatalf("create other agent: %v", err)
	}

	res, err := UploadArtifact{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, otherAgent.ID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id":       pgxToUUID(t, env.TaskID).String(),
		"slot_id":       uuid.Nil.String(),
		"execution_id":  uuid.Nil.String(),
		"artifact_type": "report",
		"title":         "denied",
		"summary":       "should fail",
		"content":       map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(res.Errors) == 0 || res.Errors[0] != "MCP_PERMISSION_DENIED" {
		t.Fatalf("expected MCP_PERMISSION_DENIED, got errors=%v note=%s", res.Errors, res.Note)
	}
}

func TestUploadArtifact_DeniesCrossWorkspaceFileIndex(t *testing.T) {
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
		t.Fatalf("create foreign workspace: %v", err)
	}
	otherUser, err := q.CreateUser(ctx, db.CreateUserParams{
		Name:  "Foreign owner " + t.Name(),
		Email: "foreign-owner+" + uniqSuffix(t) + "@example.com",
	})
	if err != nil {
		t.Fatalf("create foreign user: %v", err)
	}
	otherProject, err := q.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID:         otherWorkspace.ID,
		Title:               "Foreign project",
		Description:         pgtype.Text{String: "outside caller workspace", Valid: true},
		Status:              "running",
		ScheduleType:        "one_time",
		CronExpr:            pgtype.Text{},
		SourceConversations: []byte(`[]`),
		ChannelID:           pgtype.UUID{},
		CreatorOwnerID:      otherUser.ID,
	})
	if err != nil {
		t.Fatalf("create foreign project: %v", err)
	}

	scopeJSON, err := json.Marshal(map[string]any{"scope": "project"})
	if err != nil {
		t.Fatalf("marshal scope: %v", err)
	}
	foreignFile, err := q.CreateFileIndex(ctx, db.CreateFileIndexParams{
		WorkspaceID:          otherWorkspace.ID,
		UploaderIdentityID:   otherUser.ID,
		UploaderIdentityType: "member",
		OwnerID:              otherUser.ID,
		SourceType:           "project",
		SourceID:             otherProject.ID,
		FileName:             "foreign.pdf",
		FileSize:             pgtype.Int8{Int64: 4096, Valid: true},
		ContentType:          pgtype.Text{String: "application/pdf", Valid: true},
		StoragePath:          pgtype.Text{String: "/tmp/foreign.pdf", Valid: true},
		AccessScope:          scopeJSON,
		ChannelID:            pgtype.UUID{},
		ProjectID:            otherProject.ID,
	})
	if err != nil {
		t.Fatalf("create foreign file_index: %v", err)
	}

	res, err := UploadArtifact{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"task_id":       pgxToUUID(t, env.TaskID).String(),
		"slot_id":       uuid.Nil.String(),
		"execution_id":  uuid.Nil.String(),
		"artifact_type": "design",
		"title":         "cross-workspace file",
		"summary":       "should be denied",
		"file":          pgxToUUID(t, foreignFile.ID).String(),
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !containsError(res.Errors, errcode.MCPPermissionDenied.Code) {
		t.Fatalf("expected MCP_PERMISSION_DENIED, got errors=%v note=%s", res.Errors, res.Note)
	}
	if res.Note != "file_index belongs to a different workspace" {
		t.Fatalf("unexpected note %q", res.Note)
	}
}

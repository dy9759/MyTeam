package tools

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func TestDownloadAttachment_HappyPath(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	att, err := q.CreateAttachment(ctx, db.CreateAttachmentParams{
		WorkspaceID:  env.WorkspaceID,
		UploaderType: "member",
		UploaderID:   env.OwnerID,
		Filename:     "spec.txt",
		Url:          "https://cdn.example.com/spec.txt",
		ContentType:  "text/plain",
		SizeBytes:    1234,
		IssueID:      pgtype.UUID{},
		CommentID:    pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	res, err := DownloadAttachment{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, env.WorkspaceID),
		UserID:      pgxToUUID(t, env.OwnerID),
		AgentID:     pgxToUUID(t, env.AgentID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"attachment_id": pgxToUUID(t, att.ID).String(),
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
	if data["filename"] != "spec.txt" {
		t.Errorf("filename: want spec.txt, got %v", data["filename"])
	}
	if data["url"] != "https://cdn.example.com/spec.txt" {
		t.Errorf("url: got %v", data["url"])
	}
	if data["content_type"] != "text/plain" {
		t.Errorf("content_type: got %v", data["content_type"])
	}
	if data["size_bytes"] != int64(1234) {
		t.Errorf("size_bytes: got %v", data["size_bytes"])
	}
}

func TestDownloadAttachment_OtherWorkspace_NotFound(t *testing.T) {
	q := testDB(t)
	env := setupTaskEnv(t, q)
	ctx := context.Background()

	att, err := q.CreateAttachment(ctx, db.CreateAttachmentParams{
		WorkspaceID:  env.WorkspaceID,
		UploaderType: "member",
		UploaderID:   env.OwnerID,
		Filename:     "secret.txt",
		Url:          "https://cdn.example.com/secret.txt",
		ContentType:  "text/plain",
		SizeBytes:    10,
		IssueID:      pgtype.UUID{},
		CommentID:    pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create attachment: %v", err)
	}

	// Caller comes from a different workspace; the workspace_id WHERE
	// clause in GetAttachment must hide the row.
	otherWS, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "Other WS " + t.Name(),
		Slug:        "other-" + uniqSuffix(t),
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: "OWS",
	})
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	res, err := DownloadAttachment{}.Exec(ctx, q, mcptool.Context{
		WorkspaceID: pgxToUUID(t, otherWS.ID),
		UserID:      pgxToUUID(t, env.OwnerID),
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{
		"attachment_id": pgxToUUID(t, att.ID).String(),
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(res.Errors) == 0 || res.Errors[0] != "ATTACHMENT_NOT_FOUND" {
		t.Fatalf("expected ATTACHMENT_NOT_FOUND, got errors=%v note=%s", res.Errors, res.Note)
	}
}

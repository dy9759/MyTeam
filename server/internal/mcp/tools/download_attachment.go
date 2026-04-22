package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// DownloadAttachment returns metadata + URL for an attachment owned by the
// caller's workspace. Workspace-isolation is enforced by GetAttachmentParams
// (workspace_id in WHERE clause).
//
// The actual signed URL is produced by the HTTP layer (handler.attachmentToResponse
// uses h.CFSigner). MCP tools do not have access to that signer; we surface
// the raw storage URL plus the standard fields so the client can:
//  1. Hit the existing GET /api/attachments/{id} endpoint to fetch a signed URL, or
//  2. Use the raw URL directly when the bucket is public.
type DownloadAttachment struct{}

func (DownloadAttachment) Name() string { return "download_attachment" }

func (DownloadAttachment) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"attachment_id"},
		"properties": map[string]any{
			"attachment_id": map[string]string{"type": "string", "format": "uuid"},
		},
	}
}

func (DownloadAttachment) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (DownloadAttachment) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	attID, err := uuidArg(args, "attachment_id")
	if err != nil {
		return mcptool.Result{}, err
	}

	att, err := q.GetAttachment(ctx, db.GetAttachmentParams{
		ID:          pgUUID(attID),
		WorkspaceID: pgUUID(ws.WorkspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFoundResult("ATTACHMENT"), nil
		}
		return mcptool.Result{}, fmt.Errorf("get attachment: %w", err)
	}

	return mcptool.Result{Data: attachmentPayload(att)}, nil
}

// attachmentPayload mirrors the shape of handler.AttachmentResponse minus
// the signed download URL — that is generated only at the HTTP edge.
func attachmentPayload(a db.Attachment) map[string]any {
	out := map[string]any{
		"id":            uuid.UUID(a.ID.Bytes).String(),
		"workspace_id":  uuid.UUID(a.WorkspaceID.Bytes).String(),
		"filename":      a.Filename,
		"url":           a.Url,
		"content_type":  a.ContentType,
		"size_bytes":    a.SizeBytes,
		"uploader_type": a.UploaderType,
		"uploader_id":   uuid.UUID(a.UploaderID.Bytes).String(),
	}
	if a.IssueID.Valid {
		out["issue_id"] = uuid.UUID(a.IssueID.Bytes).String()
	}
	if a.CommentID.Valid {
		out["comment_id"] = uuid.UUID(a.CommentID.Bytes).String()
	}
	if a.CreatedAt.Valid {
		out["created_at"] = a.CreatedAt.Time
	}
	return out
}

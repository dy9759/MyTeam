package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DownloadAttachment returns a presigned URL or stream for an attachment.
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

func (DownloadAttachment) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/file.go DownloadFile
	return mcptool.Result{Stub: true, Note: "wire to handler/file.go DownloadFile"}, nil
}

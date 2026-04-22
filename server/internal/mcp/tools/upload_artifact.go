package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// UploadArtifact uploads a deliverable artifact for a task slot execution.
//
// Two paths:
//   - content (object) → headless artifact (JSONB payload only)
//   - file (uuid)      → file-backed artifact (pointer to existing file_index)
//
// Auth: caller agent must be the task's actual_agent_id OR primary_assignee_id
// (cross-cutting PRD §7.2). Human callers (ws.AgentID == nil) bypass that check
// since the dispatcher already enforced workspace membership.
type UploadArtifact struct{}

func (UploadArtifact) Name() string { return "upload_artifact" }

func (UploadArtifact) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"task_id", "slot_id", "execution_id", "artifact_type", "title", "summary"},
		"properties": map[string]any{
			"task_id":       map[string]string{"type": "string", "format": "uuid"},
			"slot_id":       map[string]string{"type": "string", "format": "uuid"},
			"execution_id":  map[string]string{"type": "string", "format": "uuid"},
			"artifact_type": map[string]string{"type": "string"},
			"title":         map[string]string{"type": "string"},
			"summary":       map[string]string{"type": "string"},
			"content":       map[string]string{"type": "object"},
			"file":          map[string]string{"type": "string", "format": "uuid"},
		},
		"oneOf": []map[string]any{
			{"required": []string{"content"}},
			{"required": []string{"file"}},
		},
	}
}

func (UploadArtifact) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (UploadArtifact) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	taskID, err := uuidArg(args, "task_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	slotID, err := optionalUUIDArg(args, "slot_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	execID, err := optionalUUIDArg(args, "execution_id")
	if err != nil {
		return mcptool.Result{}, err
	}

	artifactType := stringArg(args, "artifact_type")
	title := stringArg(args, "title")
	summary := stringArg(args, "summary")

	task, deny, err := ensureAgentOnTask(ctx, q, ws, taskID)
	if err != nil {
		return mcptool.Result{}, err
	}
	if deny.Note != "" {
		return deny, nil
	}

	// Resolve the createdBy actor from the runtime context. When the agent
	// is set we attribute to the agent; otherwise the human user.
	createdByID := ws.AgentID
	createdByType := "agent"
	if createdByID == uuid.Nil {
		createdByID = ws.UserID
		createdByType = "member"
	}

	svc := service.NewArtifactService(q)
	runID := uuid.UUID(task.RunID.Bytes)
	if !task.RunID.Valid {
		return mcptool.Result{Errors: []string{"TASK_HAS_NO_RUN"}, Note: "task has no run_id"}, nil
	}

	// File-backed path: prefer when `file` is supplied.
	if fileID, err := optionalUUIDArg(args, "file"); err != nil {
		return mcptool.Result{}, err
	} else if fileID != uuid.Nil {
		fi, err := q.GetFileIndex(ctx, pgUUID(fileID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFoundResult("FILE"), nil
			}
			return mcptool.Result{}, fmt.Errorf("get file_index: %w", err)
		}
		if !sameUUID(fi.WorkspaceID, ws.WorkspaceID) {
			return permissionDenied("file_index belongs to a different workspace"), nil
		}
		content, _ := mapArg(args, "content") // optional preview/summary
		artifact, err := svc.CreateWithFile(ctx, service.CreateWithFileRequest{
			TaskID:         taskID,
			SlotID:         slotID,
			ExecutionID:    execID,
			RunID:          runID,
			ArtifactType:   artifactType,
			Title:          title,
			Summary:        summary,
			Content:        content,
			FileIndexID:    fileID,
			RetentionClass: service.RetentionPermanent,
			CreatedByID:    createdByID,
			CreatedByType:  createdByType,
		})
		if err != nil {
			if errors.Is(err, service.ErrArtifactInvalid) {
				return mcptool.Result{Errors: []string{"ARTIFACT_INVALID"}, Note: err.Error()}, nil
			}
			return mcptool.Result{}, fmt.Errorf("create file-backed artifact: %w", err)
		}
		return mcptool.Result{Data: artifactPayload(*artifact)}, nil
	}

	// Headless path: requires content.
	content, ok := mapArg(args, "content")
	if !ok {
		return mcptool.Result{Errors: []string{"ARTIFACT_INVALID"}, Note: "either file or content is required"}, nil
	}
	artifact, err := svc.CreateHeadless(ctx, service.CreateHeadlessRequest{
		TaskID:         taskID,
		SlotID:         slotID,
		ExecutionID:    execID,
		RunID:          runID,
		ArtifactType:   artifactType,
		Title:          title,
		Summary:        summary,
		Content:        content,
		RetentionClass: service.RetentionPermanent,
		CreatedByID:    createdByID,
		CreatedByType:  createdByType,
	})
	if err != nil {
		if errors.Is(err, service.ErrArtifactInvalid) {
			return mcptool.Result{Errors: []string{"ARTIFACT_INVALID"}, Note: err.Error()}, nil
		}
		return mcptool.Result{}, fmt.Errorf("create headless artifact: %w", err)
	}
	return mcptool.Result{Data: artifactPayload(*artifact)}, nil
}

// artifactPayload is the JSON-serializable subset returned to the MCP caller.
// Mirrors the shape used by handler/artifact.go without the verbose nullable
// pgtype wrappers.
func artifactPayload(a db.Artifact) map[string]any {
	out := map[string]any{
		"id":            uuid.UUID(a.ID.Bytes).String(),
		"task_id":       uuid.UUID(a.TaskID.Bytes).String(),
		"run_id":        uuid.UUID(a.RunID.Bytes).String(),
		"artifact_type": a.ArtifactType,
		"version":       a.Version,
	}
	if a.SlotID.Valid {
		out["slot_id"] = uuid.UUID(a.SlotID.Bytes).String()
	}
	if a.ExecutionID.Valid {
		out["execution_id"] = uuid.UUID(a.ExecutionID.Bytes).String()
	}
	if a.Title.Valid {
		out["title"] = a.Title.String
	}
	if a.Summary.Valid {
		out["summary"] = a.Summary.String
	}
	if a.FileIndexID.Valid {
		out["file_index_id"] = uuid.UUID(a.FileIndexID.Bytes).String()
	}
	return out
}

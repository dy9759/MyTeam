package service

import (
	"log/slog"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// FileIndexerService listens to events that produce files and creates
// file_index entries to track all artifacts across conversations and projects.
type FileIndexerService struct {
	Queries  *db.Queries
	EventBus *events.Bus
}

// NewFileIndexerService creates a new FileIndexerService.
func NewFileIndexerService(q *db.Queries, bus *events.Bus) *FileIndexerService {
	return &FileIndexerService{
		Queries:  q,
		EventBus: bus,
	}
}

// Start subscribes to events that produce files.
func (s *FileIndexerService) Start() {
	// Subscribe to message:created - if message has file_id, index it.
	s.EventBus.Subscribe("message:created", s.handleMessageCreated)

	// Subscribe to task:completed - if result has file references, index them.
	s.EventBus.Subscribe(protocol.EventTaskCompleted, s.handleTaskCompleted)

	// Subscribe to workflow.step_completed - if output_refs has files, index them.
	s.EventBus.Subscribe(protocol.EventWorkflowStepCompleted, s.handleStepCompleted)

	slog.Info("file indexer service started")
}

// handleMessageCreated checks if a newly created message has file attachments
// and creates file_index entries for them.
// source_type = "conversation", source_id = message.channel_id
func (s *FileIndexerService) handleMessageCreated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	msgData, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}

	// Check if the message has a file_id or attachment references.
	fileID, _ := msgData["file_id"].(string)
	if fileID == "" {
		// No file attached to this message; nothing to index.
		return
	}

	channelID, _ := msgData["channel_id"].(string)
	senderID, _ := msgData["sender_id"].(string)
	senderType, _ := msgData["sender_type"].(string)

	slog.Debug("file indexer: message with file detected",
		"file_id", fileID,
		"channel_id", channelID,
		"sender_id", senderID,
		"sender_type", senderType,
		"workspace_id", e.WorkspaceID,
	)

	// TODO: Create file_index entry once the table migration exists:
	//   err := s.Queries.CreateFileIndex(ctx, db.CreateFileIndexParams{
	//       WorkspaceID:          util.ParseUUID(e.WorkspaceID),
	//       UploaderIdentityID:   util.ParseUUID(senderID),
	//       UploaderIdentityType: senderType,
	//       OwnerID:              ownerUUID, // resolve from agent.owner_id if sender is agent
	//       SourceType:           "conversation",
	//       SourceID:             util.ParseUUID(channelID),
	//       FileName:             fileName,
	//       FileSize:             fileSize,
	//       ContentType:          contentType,
	//       StoragePath:          storagePath,
	//       ChannelID:            util.ParseUUID(channelID),
	//   })
}

// handleTaskCompleted checks if a completed task has file references in its
// result and creates file_index entries for them.
func (s *FileIndexerService) handleTaskCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	taskID, _ := payload["task_id"].(string)
	result, _ := payload["result"].(map[string]any)

	// Check for file references in the result.
	files, _ := result["files"].([]any)
	if len(files) == 0 {
		return
	}

	slog.Debug("file indexer: task completed with files",
		"task_id", taskID,
		"file_count", len(files),
		"workspace_id", e.WorkspaceID,
	)

	// TODO: For each file reference, create a file_index entry:
	//   for _, f := range files {
	//       fileRef, ok := f.(map[string]any)
	//       if !ok { continue }
	//       err := s.Queries.CreateFileIndex(ctx, ...)
	//   }
}

// handleStepCompleted checks if a completed workflow step has output_refs
// containing files and creates file_index entries for them.
// source_type = "project", source_id = project_id
func (s *FileIndexerService) handleStepCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	stepID, _ := payload["step_id"].(string)

	// Check for output_refs in the step payload.
	outputRefs, _ := payload["output_refs"].([]any)
	if len(outputRefs) == 0 {
		return
	}

	slog.Debug("file indexer: workflow step completed with output files",
		"step_id", stepID,
		"output_refs_count", len(outputRefs),
		"workspace_id", e.WorkspaceID,
	)

	// TODO: For each output_ref that is a file, create a file_index entry:
	//   projectID := payload["project_id"].(string)
	//   for _, ref := range outputRefs {
	//       fileRef, ok := ref.(map[string]any)
	//       if !ok { continue }
	//       err := s.Queries.CreateFileIndex(ctx, db.CreateFileIndexParams{
	//           WorkspaceID: util.ParseUUID(e.WorkspaceID),
	//           SourceType:  "project",
	//           SourceID:    util.ParseUUID(projectID),
	//           ProjectID:   util.ParseUUID(projectID),
	//           ...
	//       })
	//   }
}

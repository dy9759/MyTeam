package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type conversationRunDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConversationAgentRunService is the MyTeam-side runtime queue used when a
// user sends a DM to a local personal agent. It intentionally mirrors the
// AgentHub runtime lifecycle (claim -> start -> events -> complete/fail), but
// writes the result back into MyTeam conversation messages.
type ConversationAgentRunService struct {
	Queries *db.Queries
	DB      conversationRunDB
	Hub     *realtime.Hub
}

func NewConversationAgentRunService(q *db.Queries, database conversationRunDB, hub *realtime.Hub) *ConversationAgentRunService {
	return &ConversationAgentRunService{Queries: q, DB: database, Hub: hub}
}

type ConversationAgentRunInput struct {
	WorkspaceID      string
	TriggerMessageID string
	AgentID          string
	RuntimeID        string
	PeerUserID       string
	Provider         string
	Prompt           string
	Metadata         map[string]any
}

type ConversationAgentRun struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	TriggerMessageID  string          `json:"trigger_message_id"`
	ResponseMessageID string          `json:"response_message_id,omitempty"`
	AgentID           string          `json:"agent_id"`
	RuntimeID         string          `json:"runtime_id"`
	PeerUserID        string          `json:"peer_user_id"`
	Provider          string          `json:"provider"`
	Status            string          `json:"status"`
	Prompt            string          `json:"prompt"`
	Output            string          `json:"output"`
	SessionID         string          `json:"session_id,omitempty"`
	WorkDir           string          `json:"work_dir,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type ConversationAgentRunEventInput struct {
	Seq      int64          `json:"seq"`
	Type     string         `json:"type"`
	Content  string         `json:"content,omitempty"`
	Tool     string         `json:"tool,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

const conversationAgentRunColumns = `
	id,
	workspace_id,
	trigger_message_id,
	response_message_id,
	agent_id,
	runtime_id,
	peer_user_id,
	provider,
	status,
	prompt,
	output,
	session_id,
	work_dir,
	error_message,
	metadata,
	created_at,
	updated_at`

func scanConversationAgentRun(row pgx.Row) (*ConversationAgentRun, error) {
	var (
		id, workspaceID, triggerMessageID, responseMessageID pgtype.UUID
		agentID, runtimeID, peerUserID                       pgtype.UUID
		provider, status, prompt, output                     string
		sessionID, workDir, errorMessage                     pgtype.Text
		metadata                                             []byte
		createdAt, updatedAt                                 pgtype.Timestamptz
	)
	if err := row.Scan(
		&id,
		&workspaceID,
		&triggerMessageID,
		&responseMessageID,
		&agentID,
		&runtimeID,
		&peerUserID,
		&provider,
		&status,
		&prompt,
		&output,
		&sessionID,
		&workDir,
		&errorMessage,
		&metadata,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	run := &ConversationAgentRun{
		ID:               util.UUIDToString(id),
		WorkspaceID:      util.UUIDToString(workspaceID),
		TriggerMessageID: util.UUIDToString(triggerMessageID),
		AgentID:          util.UUIDToString(agentID),
		RuntimeID:        util.UUIDToString(runtimeID),
		PeerUserID:       util.UUIDToString(peerUserID),
		Provider:         provider,
		Status:           status,
		Prompt:           prompt,
		Output:           output,
		Metadata:         json.RawMessage(metadata),
		CreatedAt:        createdAt.Time,
		UpdatedAt:        updatedAt.Time,
	}
	if responseMessageID.Valid {
		run.ResponseMessageID = util.UUIDToString(responseMessageID)
	}
	if sessionID.Valid {
		run.SessionID = sessionID.String
	}
	if workDir.Valid {
		run.WorkDir = workDir.String
	}
	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if len(run.Metadata) == 0 {
		run.Metadata = json.RawMessage(`{}`)
	}
	return run, nil
}

func (s *ConversationAgentRunService) EnqueueDMRun(ctx context.Context, in ConversationAgentRunInput) (*ConversationAgentRun, error) {
	if s == nil || s.DB == nil || s.Queries == nil {
		return nil, errors.New("conversation agent run service is not configured")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return nil, errors.New("conversation agent run prompt is required")
	}
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = "local"
	}
	metadata := map[string]any{
		"source":         "dm",
		"agenthub_model": "runtime_execution_queue",
	}
	for k, v := range in.Metadata {
		metadata[k] = v
	}
	metadataJSON, _ := json.Marshal(metadata)
	row := s.DB.QueryRow(ctx, `
		INSERT INTO conversation_agent_run (
			workspace_id,
			trigger_message_id,
			agent_id,
			runtime_id,
			peer_user_id,
			provider,
			status,
			prompt,
			metadata
		) VALUES ($1, $2, $3, $4, $5, $6, 'queued', $7, $8::jsonb)
		RETURNING `+conversationAgentRunColumns,
		util.ParseUUID(in.WorkspaceID),
		util.ParseUUID(in.TriggerMessageID),
		util.ParseUUID(in.AgentID),
		util.ParseUUID(in.RuntimeID),
		util.ParseUUID(in.PeerUserID),
		provider,
		prompt,
		metadataJSON,
	)
	return scanConversationAgentRun(row)
}

func (s *ConversationAgentRunService) Get(ctx context.Context, runID string) (*ConversationAgentRun, error) {
	row := s.DB.QueryRow(ctx, `SELECT `+conversationAgentRunColumns+` FROM conversation_agent_run WHERE id = $1`, util.ParseUUID(runID))
	return scanConversationAgentRun(row)
}

func (s *ConversationAgentRunService) ClaimNext(ctx context.Context, runtimeID string) (*ConversationAgentRun, error) {
	row := s.DB.QueryRow(ctx, `
		UPDATE conversation_agent_run
		SET status = 'claimed', claimed_at = now(), updated_at = now()
		WHERE id = (
			SELECT id
			FROM conversation_agent_run
			WHERE runtime_id = $1 AND status = 'queued'
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+conversationAgentRunColumns,
		util.ParseUUID(runtimeID),
	)
	run, err := scanConversationAgentRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (s *ConversationAgentRunService) MarkRunning(ctx context.Context, runID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE conversation_agent_run
		SET status = 'running', started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE id = $1 AND status IN ('claimed', 'queued')`,
		util.ParseUUID(runID),
	)
	return err
}

func (s *ConversationAgentRunService) AppendEvents(ctx context.Context, runID string, events []ConversationAgentRunEventInput) error {
	if len(events) == 0 {
		return nil
	}
	var visible strings.Builder
	for _, evt := range events {
		typ := strings.TrimSpace(evt.Type)
		if typ == "" {
			typ = "event"
		}
		metadataJSON, _ := json.Marshal(nonNilMap(evt.Metadata))
		var inputJSON any
		if evt.Input != nil {
			encoded, _ := json.Marshal(evt.Input)
			inputJSON = encoded
		}
		if _, err := s.DB.Exec(ctx, `
			INSERT INTO conversation_agent_run_event (
				run_id, seq, type, content, tool, input, output, error, metadata
			) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, NULLIF($7, ''), NULLIF($8, ''), $9::jsonb)
			ON CONFLICT (run_id, seq) DO NOTHING`,
			util.ParseUUID(runID),
			evt.Seq,
			typ,
			nullableText(evt.Content),
			evt.Tool,
			inputJSON,
			evt.Output,
			evt.Error,
			metadataJSON,
		); err != nil {
			return err
		}
		if text := visibleTextFromEvent(typ, evt); text != "" {
			visible.WriteString(text)
		}
	}
	if visible.Len() > 0 {
		return s.appendTextToResponseMessage(ctx, runID, visible.String())
	}
	return nil
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func visibleTextFromEvent(typ string, evt ConversationAgentRunEventInput) string {
	switch typ {
	case "text", "assistant_message", "assistant_delta":
		return evt.Content
	default:
		return ""
	}
}

func (s *ConversationAgentRunService) appendTextToResponseMessage(ctx context.Context, runID string, text string) error {
	if text == "" {
		return nil
	}
	run, err := s.Get(ctx, runID)
	if err != nil {
		return err
	}
	patchJSON, _ := json.Marshal(map[string]any{
		"local_agent_run_id": run.ID,
		"streaming":          true,
		"source":             "local_agent",
	})
	if run.ResponseMessageID == "" {
		msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
			WorkspaceID:   util.ParseUUID(run.WorkspaceID),
			SenderID:      util.ParseUUID(run.AgentID),
			SenderType:    "agent",
			RecipientID:   util.ParseUUID(run.PeerUserID),
			RecipientType: util.StrToText("member"),
			Content:       text,
			ContentType:   "text",
			Type:          "agent_reply",
			Metadata:      patchJSON,
		})
		if err != nil {
			return err
		}
		if _, err := s.DB.Exec(ctx, `
			UPDATE conversation_agent_run
			SET response_message_id = $2, output = output || $3, updated_at = now()
			WHERE id = $1`,
			util.ParseUUID(run.ID),
			msg.ID,
			text,
		); err != nil {
			return err
		}
		s.broadcastMessage(run.WorkspaceID, "message:created", msg)
		return nil
	}
	if _, err := s.DB.Exec(ctx, `
		UPDATE message
		SET content = content || $2,
			metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
			updated_at = now()
		WHERE id = $1`,
		util.ParseUUID(run.ResponseMessageID),
		text,
		patchJSON,
	); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ctx, `
		UPDATE conversation_agent_run
		SET output = output || $2, updated_at = now()
		WHERE id = $1`,
		util.ParseUUID(run.ID),
		text,
	); err != nil {
		return err
	}
	msg, err := s.Queries.GetMessage(ctx, util.ParseUUID(run.ResponseMessageID))
	if err != nil {
		return err
	}
	s.broadcastMessage(run.WorkspaceID, "message:updated", msg)
	return nil
}

func (s *ConversationAgentRunService) Complete(ctx context.Context, runID, output, sessionID, workDir string) error {
	run, err := s.Get(ctx, runID)
	if err != nil {
		return err
	}
	if run.ResponseMessageID == "" && strings.TrimSpace(output) != "" {
		if err := s.appendTextToResponseMessage(ctx, runID, output); err != nil {
			return err
		}
	}
	_, err = s.DB.Exec(ctx, `
		UPDATE conversation_agent_run
		SET status = 'completed',
			output = CASE WHEN output = '' AND $2 <> '' THEN $2 ELSE output END,
			session_id = NULLIF($3, ''),
			work_dir = NULLIF($4, ''),
			completed_at = now(),
			updated_at = now()
		WHERE id = $1`,
		util.ParseUUID(runID),
		output,
		sessionID,
		workDir,
	)
	if err != nil {
		return err
	}
	return s.markStreaming(ctx, runID, false)
}

func (s *ConversationAgentRunService) Fail(ctx context.Context, runID, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "local agent execution failed"
	}
	_, err := s.DB.Exec(ctx, `
		UPDATE conversation_agent_run
		SET status = 'failed', error_message = $2, completed_at = now(), updated_at = now()
		WHERE id = $1`,
		util.ParseUUID(runID),
		message,
	)
	if err != nil {
		return err
	}
	run, getErr := s.Get(ctx, runID)
	if getErr == nil && run.ResponseMessageID == "" {
		_ = s.appendTextToResponseMessage(ctx, runID, "本地 Agent 执行失败："+message)
	}
	return s.markStreaming(ctx, runID, false)
}

func (s *ConversationAgentRunService) Status(ctx context.Context, runID string) (string, error) {
	var status string
	err := s.DB.QueryRow(ctx, `SELECT status FROM conversation_agent_run WHERE id = $1`, util.ParseUUID(runID)).Scan(&status)
	return status, err
}

func (s *ConversationAgentRunService) markStreaming(ctx context.Context, runID string, streaming bool) error {
	run, err := s.Get(ctx, runID)
	if err != nil {
		return err
	}
	if run.ResponseMessageID == "" {
		return nil
	}
	patchJSON, _ := json.Marshal(map[string]any{
		"local_agent_run_id": run.ID,
		"streaming":          streaming,
		"source":             "local_agent",
	})
	if _, err := s.DB.Exec(ctx, `
		UPDATE message
		SET metadata = COALESCE(metadata, '{}'::jsonb) || $2::jsonb,
			updated_at = now()
		WHERE id = $1`,
		util.ParseUUID(run.ResponseMessageID),
		patchJSON,
	); err != nil {
		return err
	}
	msg, err := s.Queries.GetMessage(ctx, util.ParseUUID(run.ResponseMessageID))
	if err != nil {
		return err
	}
	s.broadcastMessage(run.WorkspaceID, "message:updated", msg)
	return nil
}

func (s *ConversationAgentRunService) broadcastMessage(workspaceID string, eventType string, msg db.Message) {
	if s == nil || s.Hub == nil {
		return
	}
	data, err := json.Marshal(map[string]any{"type": eventType, "payload": messageToMap(msg)})
	if err != nil {
		slog.Warn("conversation run: marshal message event failed", "error", err)
		return
	}
	s.Hub.BroadcastToWorkspace(workspaceID, data)
}

func (s *ConversationAgentRunService) String() string {
	return fmt.Sprintf("ConversationAgentRunService{%t}", s != nil && s.DB != nil)
}

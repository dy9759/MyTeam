// Package service: meeting.go — orchestrates a meeting kind=thread through
// its lifecycle (planned → recording → transcribing → summarized →
// completed). Designed against an ASR interface so the unit tests can
// drop in a deterministic mock; production wires the Doubao 妙记 batch
// client.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service/asr"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MeetingStatus mirrors thread.metadata.meeting_status. Pure runtime value,
// no DB CHECK constraint (the meeting kind itself is opt-in).
type MeetingStatus string

const (
	MeetingPlanned      MeetingStatus = "planned"
	MeetingRecording    MeetingStatus = "recording"
	MeetingTranscribing MeetingStatus = "transcribing"
	MeetingSummarized   MeetingStatus = "summarized"
	MeetingCompleted    MeetingStatus = "completed"
	MeetingCancelled    MeetingStatus = "cancelled"
)

// ActionItemStatus tracks the half-auto approval flow. Stored on the
// thread_context_item.metadata JSON so we can keep using the existing
// item table.
type ActionItemStatus string

const (
	ActionItemPending  ActionItemStatus = "pending"
	ActionItemApproved ActionItemStatus = "approved"
	ActionItemRejected ActionItemStatus = "rejected"
)

// AutoApproveThreshold is the confidence cutoff above which an action item
// could be auto-promoted. The MVP keeps everything half-auto: even at
// confidence=1 we still wait for an explicit ApproveActionItem call.
const AutoApproveThreshold = 0.85

// MeetingMeta is the typed projection of thread.metadata when kind=meeting.
// Persistence is JSON; this struct is the only place the field names are
// defined.
type MeetingMeta struct {
	Kind             string         `json:"kind"`
	MeetingStatus    MeetingStatus  `json:"meeting_status"`
	AudioFileID      string         `json:"audio_file_id,omitempty"`
	TranscriptFileID string         `json:"transcript_file_id,omitempty"`
	Agenda           []string       `json:"agenda,omitempty"`
	Briefing         map[string]any `json:"briefing,omitempty"`
	Summary          map[string]any `json:"summary,omitempty"`
	ASRProvider      string         `json:"asr_provider,omitempty"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	EndedAt          *time.Time     `json:"ended_at,omitempty"`
}

// SecretGetter is the contract MeetingService uses to fetch Feishu
// credentials per workspace. SecretService satisfies it.
type SecretGetter interface {
	GetPlaintext(ctx context.Context, workspaceID uuid.UUID, key string) (string, error)
}

// MeetingService orchestrates the meeting flow. ASR is an interface so
// tests can inject a fixed-response mock; production wires Doubao 妙记.
type MeetingService struct {
	Q       *db.Queries
	Secrets SecretGetter
	ASR     asr.Client
}

func NewMeetingService(q *db.Queries, secrets SecretGetter, asrClient asr.Client) *MeetingService {
	return &MeetingService{Q: q, Secrets: secrets, ASR: asrClient}
}

// ErrNotMeeting / ErrAudioMissing are sentinel errors callers can branch on
// to map cleanly to HTTP error codes.
var (
	ErrNotMeeting   = errors.New("thread is not a meeting")
	ErrAudioMissing = errors.New("meeting has no audio attached")
	ErrItemNotFound = errors.New("action item not found")
)

// StartMeeting upgrades an existing thread to kind=meeting. Idempotent:
// calling it again with a different agenda overwrites the agenda but
// keeps existing audio/summary state.
func (s *MeetingService) StartMeeting(ctx context.Context, threadID uuid.UUID, agenda []string) (MeetingMeta, error) {
	meta, _, err := s.loadMeta(ctx, threadID)
	if err != nil && !errors.Is(err, ErrNotMeeting) {
		return MeetingMeta{}, err
	}
	now := time.Now().UTC()
	meta.Kind = "meeting"
	if meta.MeetingStatus == "" {
		meta.MeetingStatus = MeetingPlanned
	}
	meta.Agenda = agenda
	if meta.StartedAt == nil {
		meta.StartedAt = &now
	}
	if err := s.writeMeta(ctx, threadID, meta); err != nil {
		return MeetingMeta{}, err
	}
	return meta, nil
}

// AttachAudio records the file_index id of the meeting recording and
// flips status to transcribing. Caller is responsible for the actual
// upload (handler/file_index.go path).
func (s *MeetingService) AttachAudio(ctx context.Context, threadID, fileIndexID uuid.UUID) (MeetingMeta, error) {
	meta, _, err := s.loadMeta(ctx, threadID)
	if err != nil {
		return MeetingMeta{}, err
	}
	if meta.Kind != "meeting" {
		return MeetingMeta{}, ErrNotMeeting
	}
	meta.AudioFileID = fileIndexID.String()
	meta.MeetingStatus = MeetingTranscribing
	if err := s.writeMeta(ctx, threadID, meta); err != nil {
		return MeetingMeta{}, err
	}
	return meta, nil
}

// Summarize is the heavy step: load creds + audio URL → call ASR →
// persist summary + each action item as a thread_context_item with
// status=pending. Returns the bundle for inspection.
func (s *MeetingService) Summarize(ctx context.Context, threadID uuid.UUID, audioURL string) (asr.SummaryBundle, error) {
	meta, thread, err := s.loadMeta(ctx, threadID)
	if err != nil {
		return asr.SummaryBundle{}, err
	}
	if meta.Kind != "meeting" {
		return asr.SummaryBundle{}, ErrNotMeeting
	}
	if meta.AudioFileID == "" && audioURL == "" {
		return asr.SummaryBundle{}, ErrAudioMissing
	}

	creds, err := s.loadCredentials(ctx, uuid.UUID(thread.WorkspaceID.Bytes))
	if err != nil {
		return asr.SummaryBundle{}, fmt.Errorf("load credentials: %w", err)
	}

	bundle, err := s.ASR.BatchSummarize(ctx, creds, audioURL)
	if err != nil {
		return asr.SummaryBundle{}, fmt.Errorf("asr: %w", err)
	}

	// Persist summary on the thread.
	meta.Summary = map[string]any{
		"sections":  bundle.Sections,
		"decisions": bundle.Decisions,
	}
	meta.ASRProvider = bundle.Provider
	meta.MeetingStatus = MeetingSummarized
	now := time.Now().UTC()
	meta.EndedAt = &now
	if err := s.writeMeta(ctx, threadID, meta); err != nil {
		return asr.SummaryBundle{}, err
	}

	// Persist each action item as a context_item row.
	for _, ai := range bundle.ActionItems {
		bodyBytes, _ := json.Marshal(ai)
		itemMeta, _ := json.Marshal(map[string]any{
			"status":     ActionItemPending,
			"confidence": ai.Confidence,
			"owner_hint": ai.Owner,
		})
		if _, err := s.Q.CreateThreadContextItem(ctx, db.CreateThreadContextItemParams{
			WorkspaceID:    thread.WorkspaceID,
			ThreadID:       toPgUUIDFromBytes(thread.ID),
			ItemType:       "action_item",
			Title:          pgtype.Text{String: meetingTruncate(ai.Task, 200), Valid: true},
			Body:           pgtype.Text{String: string(bodyBytes), Valid: true},
			Metadata:       itemMeta,
			RetentionClass: pgtype.Text{String: "permanent", Valid: true},
			CreatedByType:  pgtype.Text{String: "system", Valid: true},
		}); err != nil {
			return bundle, fmt.Errorf("persist action item %q: %w", ai.Task, err)
		}
	}
	return bundle, nil
}

// ApproveActionItem creates a Task on the given plan with the supplied
// primary assignee, and marks the item.metadata.status=approved with a
// task_id pointer back. The half-auto step.
func (s *MeetingService) ApproveActionItem(ctx context.Context, itemID, planID, primaryAssigneeID uuid.UUID) (db.Task, error) {
	item, err := s.Q.GetThreadContextItem(ctx, pgUUIDFromUUID(itemID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Task{}, ErrItemNotFound
		}
		return db.Task{}, err
	}
	var ai asr.ActionItem
	if err := json.Unmarshal([]byte(item.Body.String), &ai); err != nil {
		return db.Task{}, fmt.Errorf("decode action item body: %w", err)
	}

	plan, err := s.Q.GetPlan(ctx, pgUUIDFromUUID(planID))
	if err != nil {
		return db.Task{}, fmt.Errorf("plan not found: %w", err)
	}

	task, err := s.Q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            plan.ID,
		WorkspaceID:       plan.WorkspaceID,
		Title:             ai.Task,
		Description:       pgtype.Text{String: ai.Owner + " — from meeting", Valid: ai.Owner != ""},
		StepOrder:         pgtype.Int4{Int32: 1, Valid: true},
		PrimaryAssigneeID: pgUUIDFromUUID(primaryAssigneeID),
		DependsOn:         []pgtype.UUID{},
		FallbackAgentIds:  []pgtype.UUID{},
		RequiredSkills:    []string{},
	})
	if err != nil {
		return db.Task{}, fmt.Errorf("create task: %w", err)
	}

	// Patch the item metadata to reflect approval + task linkage.
	itemMeta := mustMeta(item.Metadata)
	itemMeta["status"] = ActionItemApproved
	itemMeta["task_id"] = uuid.UUID(task.ID.Bytes).String()
	itemMeta["approved_at"] = time.Now().UTC()
	mb, _ := json.Marshal(itemMeta)
	if err := s.Q.UpdateThreadContextItemMetadata(ctx, db.UpdateThreadContextItemMetadataParams{
		ID:       item.ID,
		Metadata: mb,
	}); err != nil {
		// Task is already created; failing the metadata update would
		// leave a duplicate-on-retry risk. Surface so caller can decide.
		return task, fmt.Errorf("task created but metadata update failed: %w", err)
	}
	return task, nil
}

// RejectActionItem tombstones an item without creating a task.
func (s *MeetingService) RejectActionItem(ctx context.Context, itemID uuid.UUID) error {
	item, err := s.Q.GetThreadContextItem(ctx, pgUUIDFromUUID(itemID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrItemNotFound
		}
		return err
	}
	itemMeta := mustMeta(item.Metadata)
	itemMeta["status"] = ActionItemRejected
	itemMeta["rejected_at"] = time.Now().UTC()
	mb, _ := json.Marshal(itemMeta)
	return s.Q.UpdateThreadContextItemMetadata(ctx, db.UpdateThreadContextItemMetadataParams{
		ID:       item.ID,
		Metadata: mb,
	})
}

// ListActionItems returns every action_item context_item for a thread, in
// creation order, decoded into typed records.
type ActionItemView struct {
	ID         uuid.UUID        `json:"id"`
	Task       string           `json:"task"`
	Owner      string           `json:"owner,omitempty"`
	DueDate    string           `json:"due_date,omitempty"`
	Confidence float64          `json:"confidence"`
	Status     ActionItemStatus `json:"status"`
	TaskID     string           `json:"task_id,omitempty"`
}

func (s *MeetingService) ListActionItems(ctx context.Context, threadID uuid.UUID) ([]ActionItemView, error) {
	items, err := s.Q.ListThreadContextItemsByType(ctx, db.ListThreadContextItemsByTypeParams{
		ThreadID: pgUUIDFromUUID(threadID),
		ItemType: "action_item",
	})
	if err != nil {
		return nil, err
	}
	out := make([]ActionItemView, 0, len(items))
	for _, it := range items {
		var ai asr.ActionItem
		_ = json.Unmarshal([]byte(it.Body.String), &ai)
		meta := mustMeta(it.Metadata)
		view := ActionItemView{
			ID:         uuid.UUID(it.ID.Bytes),
			Task:       ai.Task,
			Owner:      ai.Owner,
			DueDate:    ai.DueDate,
			Confidence: ai.Confidence,
			Status:     ActionItemPending,
		}
		if s, ok := meta["status"].(string); ok {
			view.Status = ActionItemStatus(s)
		}
		if t, ok := meta["task_id"].(string); ok {
			view.TaskID = t
		}
		out = append(out, view)
	}
	return out, nil
}

// loadMeta fetches a thread + decodes its meeting metadata. Returns
// ErrNotMeeting when kind != "meeting" so callers can decide whether
// that's a hard failure or just a "first-time setup".
func (s *MeetingService) loadMeta(ctx context.Context, threadID uuid.UUID) (MeetingMeta, db.Thread, error) {
	thread, err := s.Q.GetThread(ctx, pgUUIDFromUUID(threadID))
	if err != nil {
		return MeetingMeta{}, db.Thread{}, fmt.Errorf("get thread: %w", err)
	}
	var meta MeetingMeta
	if len(thread.Metadata) > 0 {
		_ = json.Unmarshal(thread.Metadata, &meta)
	}
	if meta.Kind != "meeting" {
		return meta, thread, ErrNotMeeting
	}
	return meta, thread, nil
}

func (s *MeetingService) writeMeta(ctx context.Context, threadID uuid.UUID, meta MeetingMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return s.Q.UpdateThreadMetadata(ctx, db.UpdateThreadMetadataParams{
		ID:       pgUUIDFromUUID(threadID),
		Metadata: b,
	})
}

func (s *MeetingService) loadCredentials(ctx context.Context, workspaceID uuid.UUID) (asr.Credentials, error) {
	app, err := s.Secrets.GetPlaintext(ctx, workspaceID, "feishu_app_id")
	if err != nil {
		return asr.Credentials{}, fmt.Errorf("feishu_app_id: %w", err)
	}
	tok, err := s.Secrets.GetPlaintext(ctx, workspaceID, "feishu_access_token")
	if err != nil {
		return asr.Credentials{}, fmt.Errorf("feishu_access_token: %w", err)
	}
	sec, err := s.Secrets.GetPlaintext(ctx, workspaceID, "feishu_secret_key")
	if err != nil {
		return asr.Credentials{}, fmt.Errorf("feishu_secret_key: %w", err)
	}
	return asr.Credentials{AppID: app, AccessToken: tok, SecretKey: sec}, nil
}

// --- helpers (intentionally kept local — these don't deserve their own
// utility file given they're meeting-only).

func toPgUUIDFromBytes(id pgtype.UUID) pgtype.UUID { return id }

func pgUUIDFromUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func mustMeta(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func meetingTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

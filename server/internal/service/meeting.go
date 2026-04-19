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
	"io"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service/asr"
	"github.com/multica-ai/multica/server/internal/service/memory"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter mirrors handler.txStarter — anything with a Begin(ctx) method
// returning pgx.Tx. Production wires *pgxpool.Pool; tests can drop in a
// pgx.Conn or a fake.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

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
//
// Tx is optional but strongly recommended in production — without it,
// Summarize and ApproveActionItem fall back to non-transactional writes,
// which leaves a partial-write window if the second statement fails.
type MeetingService struct {
	Q       *db.Queries
	Tx      TxStarter
	Secrets SecretGetter
	ASR     asr.Client
	Memory  *memory.Service  // optional; nil disables dual-write to memory_record
	Storage storage.Storage  // optional; required by UploadAudio + auto-presign in Summarize
}

func NewMeetingService(q *db.Queries, tx TxStarter, secrets SecretGetter, asrClient asr.Client) *MeetingService {
	return &MeetingService{Q: q, Tx: tx, Secrets: secrets, ASR: asrClient}
}

// WithMemory enables dual-write into memory_record for every action_item
// + the meeting summary itself. Pass nil to disable (default).
func (s *MeetingService) WithMemory(m *memory.Service) *MeetingService {
	s.Memory = m
	return s
}

// WithStorage enables UploadAudio + auto-presign in Summarize. Storage
// is per-workspace; caller resolves via storage.Factory.NewFromWorkspace.
func (s *MeetingService) WithStorage(st storage.Storage) *MeetingService {
	s.Storage = st
	return s
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

// UploadAudio uploads the recording bytes to the workspace's Storage
// backend, creates a file_index row, then records the id on the
// meeting (status → transcribing). Returns the file_index id so the
// caller can verify or pass it back as audioURL override later.
//
// Requires Storage to be wired (WithStorage). Returns an error
// otherwise — non-storage callers must use AttachAudio directly with
// a pre-existing file_index id.
func (s *MeetingService) UploadAudio(ctx context.Context, threadID, ownerID uuid.UUID, body io.Reader, filename, contentType string) (MeetingMeta, uuid.UUID, error) {
	if s.Storage == nil {
		return MeetingMeta{}, uuid.Nil, errors.New("meeting: Storage not wired (use WithStorage or AttachAudio)")
	}
	meta, thread, err := s.loadMeta(ctx, threadID)
	if err != nil {
		return MeetingMeta{}, uuid.Nil, err
	}
	if meta.Kind != "meeting" {
		return MeetingMeta{}, uuid.Nil, ErrNotMeeting
	}
	wsID := uuid.UUID(thread.WorkspaceID.Bytes)
	key := fmt.Sprintf("meetings/%s/%s-%s",
		wsID.String(), threadID.String(), uuid.NewString()[:8])
	if filename != "" {
		key += "-" + filename
	}
	storagePath, err := s.Storage.Put(ctx, key, body, contentType, filename)
	if err != nil {
		return MeetingMeta{}, uuid.Nil, fmt.Errorf("storage put: %w", err)
	}
	row, err := s.Q.CreateFileIndex(ctx, db.CreateFileIndexParams{
		WorkspaceID:          thread.WorkspaceID,
		UploaderIdentityID:   pgUUIDFromUUID(ownerID),
		UploaderIdentityType: "member",
		OwnerID:              pgUUIDFromUUID(ownerID),
		SourceType:           "thread",
		SourceID:             thread.ID,
		FileName:             filename,
		FileSize:             pgtype.Int8{Valid: true}, // size unknown post-stream; ok for MVP
		ContentType:          pgtype.Text{String: contentType, Valid: contentType != ""},
		StoragePath:          pgtype.Text{String: storagePath, Valid: true},
		AccessScope:          []byte(`{"scope":"channel"}`),
		ChannelID:            thread.ChannelID,
	})
	if err != nil {
		return MeetingMeta{}, uuid.Nil, fmt.Errorf("create file_index: %w", err)
	}
	fileID := uuid.UUID(row.ID.Bytes)
	updated, err := s.AttachAudio(ctx, threadID, fileID)
	if err != nil {
		return MeetingMeta{}, fileID, err
	}
	return updated, fileID, nil
}

// AttachAudio records the file_index id of the meeting recording and
// flips status to transcribing. Caller is responsible for the actual
// upload (handler/file_index.go path or UploadAudio).
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

	// Auto-presign the attached audio when caller didn't override.
	// Doubao 妙记 fetches the URL server-side, so it must reach our
	// bucket — presigned GET is the safe path that doesn't expose
	// our keys. 30-min TTL > typical poll wall clock.
	if audioURL == "" && meta.AudioFileID != "" {
		if s.Storage == nil {
			return asr.SummaryBundle{}, errors.New("meeting: Storage not wired; cannot auto-presign audio_file_id")
		}
		fileID, err := uuid.Parse(meta.AudioFileID)
		if err != nil {
			return asr.SummaryBundle{}, fmt.Errorf("parse audio_file_id: %w", err)
		}
		fi, err := s.Q.GetFileIndex(ctx, pgUUIDFromUUID(fileID))
		if err != nil {
			return asr.SummaryBundle{}, fmt.Errorf("load file_index %s: %w", fileID, err)
		}
		audioURL, err = s.Storage.Presign(ctx, fi.StoragePath.String, 30*time.Minute)
		if err != nil {
			return asr.SummaryBundle{}, fmt.Errorf("presign audio: %w", err)
		}
	}

	creds, err := s.loadCredentials(ctx, uuid.UUID(thread.WorkspaceID.Bytes))
	if err != nil {
		return asr.SummaryBundle{}, fmt.Errorf("load credentials: %w", err)
	}

	bundle, err := s.ASR.BatchSummarize(ctx, creds, audioURL)
	if err != nil {
		return asr.SummaryBundle{}, fmt.Errorf("asr: %w", err)
	}

	// Build the post-summary metadata (kept out of the tx scope so we can
	// reuse it both inside and outside the tx branch).
	meta.Summary = map[string]any{
		"sections":  bundle.Sections,
		"decisions": bundle.Decisions,
	}
	meta.ASRProvider = bundle.Provider
	meta.MeetingStatus = MeetingSummarized
	now := time.Now().UTC()
	meta.EndedAt = &now
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return asr.SummaryBundle{}, fmt.Errorf("marshal meta: %w", err)
	}

	// Transactional path: thread metadata + every action_item insert +
	// optional memory_record dual-write committed atomically. Without
	// Tx the writes happen sequentially and a mid-stream failure leaves
	// the thread marked summarized but missing action items.
	persist := func(q *db.Queries, memorySvc *memory.Service) error {
		if err := q.UpdateThreadMetadata(ctx, db.UpdateThreadMetadataParams{
			ID:       pgUUIDFromUUID(threadID),
			Metadata: metaBytes,
		}); err != nil {
			return fmt.Errorf("update thread metadata: %w", err)
		}
		for _, ai := range bundle.ActionItems {
			bodyBytes, _ := json.Marshal(ai)
			itemMeta, _ := json.Marshal(map[string]any{
				"status":     ActionItemPending,
				"confidence": ai.Confidence,
				"owner_hint": ai.Owner,
			})
			ctxItem, err := q.CreateThreadContextItem(ctx, db.CreateThreadContextItemParams{
				WorkspaceID:    thread.WorkspaceID,
				ThreadID:       toPgUUIDFromBytes(thread.ID),
				ItemType:       "action_item",
				Title:          pgtype.Text{String: meetingTruncate(ai.Task, 200), Valid: true},
				Body:           pgtype.Text{String: string(bodyBytes), Valid: true},
				Metadata:       itemMeta,
				RetentionClass: pgtype.Text{String: "permanent", Valid: true},
				CreatedByType:  pgtype.Text{String: "system", Valid: true},
			})
			if err != nil {
				return fmt.Errorf("persist action item %q: %w", ai.Task, err)
			}
			// Phase 2 dual-write: parallel memory_record row pointing at
			// this thread_context_item. Default status=candidate (per
			// reference §七.4 — agents write candidates, humans confirm).
			//
			// Phase S: auto-promote when ai.Confidence >= AutoApproveThreshold
			// (0.85). Skip the human gate for high-confidence extractions —
			// they trigger the full Promote pipeline (auto-index + Bus
			// memory.confirmed event + downstream sync).
			if memorySvc != nil {
				appended, err := memorySvc.Append(ctx, memory.AppendInput{
					WorkspaceID: uuid.UUID(thread.WorkspaceID.Bytes),
					Type:        memory.TypeTask,
					Scope:       memory.ScopeSharedSummary,
					Source:      "meeting",
					Raw: memory.RawRef{
						Kind: memory.RawThreadContextItem,
						ID:   uuid.UUID(ctxItem.ID.Bytes),
					},
					Summary:    meetingTruncate(ai.Task, 200),
					Body:       string(bodyBytes),
					Confidence: ai.Confidence,
					Status:     memory.StatusCandidate,
				})
				if err != nil {
					return fmt.Errorf("dual-write memory_record for action %q: %w", ai.Task, err)
				}
				if ai.Confidence >= AutoApproveThreshold {
					if _, err := memorySvc.Promote(ctx, appended.ID); err != nil {
						// Don't fail the meeting on auto-promote error —
						// item is still in DB as candidate, human can
						// approve manually.
						slog.Warn("meeting: auto-promote failed",
							"action", ai.Task, "confidence", ai.Confidence, "err", err)
					}
				}
			}
		}
		return nil
	}

	if s.Tx != nil {
		tx, err := s.Tx.Begin(ctx)
		if err != nil {
			return asr.SummaryBundle{}, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)
		txQ := s.Q.WithTx(tx)
		var txMem *memory.Service
		if s.Memory != nil {
			txMem = memory.NewService(txQ)
		}
		if err := persist(txQ, txMem); err != nil {
			return asr.SummaryBundle{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return asr.SummaryBundle{}, fmt.Errorf("commit: %w", err)
		}
		return bundle, nil
	}
	// No-tx fallback (test wiring without a pool, or callers that
	// explicitly opted out). Same writes, no atomicity.
	if err := persist(s.Q, s.Memory); err != nil {
		return bundle, err
	}
	return bundle, nil
}

// ApproveActionItem creates a Task on the given plan with the supplied
// primary assignee, and marks the item.metadata.status=approved with a
// task_id pointer back. Half-auto step.
//
// Atomicity: when Tx is wired, the entire (lookup → idempotency check →
// CreateTask → UpdateMetadata) sequence runs in one tx. The idempotency
// check returns the previously-created task on retry instead of inserting
// a duplicate. Without Tx the same logic runs without a row lock so a
// concurrent Approve race could double-create — production must wire Tx.
func (s *MeetingService) ApproveActionItem(ctx context.Context, itemID, planID, primaryAssigneeID uuid.UUID) (db.Task, error) {
	approve := func(q *db.Queries) (db.Task, error) {
		item, err := q.GetThreadContextItem(ctx, pgUUIDFromUUID(itemID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Task{}, ErrItemNotFound
			}
			return db.Task{}, err
		}

		// Idempotency: if this item already has task_id, return that task
		// instead of creating a new one. Survives both retries-after-error
		// and double-approve UI clicks.
		itemMeta := mustMeta(item.Metadata)
		if existing, ok := itemMeta["task_id"].(string); ok && existing != "" {
			tid, parseErr := uuid.Parse(existing)
			if parseErr == nil {
				if existingTask, err := q.GetTask(ctx, pgUUIDFromUUID(tid)); err == nil {
					return existingTask, nil
				}
			}
		}

		var ai asr.ActionItem
		if err := json.Unmarshal([]byte(item.Body.String), &ai); err != nil {
			return db.Task{}, fmt.Errorf("decode action item body: %w", err)
		}
		plan, err := q.GetPlan(ctx, pgUUIDFromUUID(planID))
		if err != nil {
			return db.Task{}, fmt.Errorf("plan not found: %w", err)
		}
		task, err := q.CreateTask(ctx, db.CreateTaskParams{
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
		itemMeta["status"] = ActionItemApproved
		itemMeta["task_id"] = uuid.UUID(task.ID.Bytes).String()
		itemMeta["approved_at"] = time.Now().UTC()
		mb, _ := json.Marshal(itemMeta)
		if err := q.UpdateThreadContextItemMetadata(ctx, db.UpdateThreadContextItemMetadataParams{
			ID:       item.ID,
			Metadata: mb,
		}); err != nil {
			return db.Task{}, fmt.Errorf("update item metadata: %w", err)
		}
		return task, nil
	}

	if s.Tx != nil {
		tx, err := s.Tx.Begin(ctx)
		if err != nil {
			return db.Task{}, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)
		task, err := approve(s.Q.WithTx(tx))
		if err != nil {
			return db.Task{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return db.Task{}, fmt.Errorf("commit: %w", err)
		}
		return task, nil
	}
	return approve(s.Q)
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
		if err := json.Unmarshal(thread.Metadata, &meta); err != nil {
			// A corrupt metadata blob silently looks like "no meeting"
			// to the caller and StartMeeting will overwrite it. Loud
			// warn so we notice during dev/staging.
			slog.Warn("meeting: thread.metadata decode failed",
				"thread_id", uuid.UUID(thread.ID.Bytes).String(),
				"err", err)
		}
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

// meetingTruncate caps a string at n bytes without splitting a UTF-8
// rune. Action-item titles are commonly Chinese; naive byte-slicing
// produces invalid UTF-8 that DB drivers may silently mangle.
func meetingTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	out := s[:n]
	for len(out) > 0 && !utf8.ValidString(out) {
		out = out[:len(out)-1]
	}
	return out
}

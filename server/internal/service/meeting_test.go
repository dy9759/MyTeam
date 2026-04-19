package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service/asr"
	"github.com/multica-ai/multica/server/internal/service/memory"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeASR returns a deterministic SummaryBundle. Tests inject it instead
// of the real Doubao client so we don't need network or credentials.
type fakeASR struct {
	bundle asr.SummaryBundle
	err    error
}

func (f *fakeASR) BatchSummarize(_ context.Context, _ asr.Credentials, _ string) (asr.SummaryBundle, error) {
	return f.bundle, f.err
}

// fakeSecrets returns canned credentials regardless of workspace.
type fakeSecrets struct{}

func (fakeSecrets) GetPlaintext(_ context.Context, _ uuid.UUID, key string) (string, error) {
	switch key {
	case "feishu_app_id":
		return "app", nil
	case "feishu_access_token":
		return "tok", nil
	case "feishu_secret_key":
		return "sec", nil
	}
	return "", nil
}

func TestMeetingService_HappyPath(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	// --- fixtures: workspace + user + channel + thread ---
	wsID := createTestWorkspace(t, q)
	userID := createTestUser(t, q, "meeting@example.com", "Meeting User")
	if _, err := q.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: wsID,
		UserID:      userID,
		Role:        "owner",
	}); err != nil {
		t.Fatalf("create member: %v", err)
	}
	ch, err := q.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID:   wsID,
		Name:          "meeting-test-" + uuid.NewString()[:8],
		Description:   pgtype.Text{String: "test channel", Valid: true},
		CreatedBy:     userID,
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	thread, err := q.CreateThread(ctx, db.CreateThreadParams{
		ChannelID:     ch.ID,
		WorkspaceID:   wsID,
		Title:         pgtype.Text{String: "Sprint review", Valid: true},
		Status:        pgtype.Text{String: "active", Valid: true},
		CreatedBy:     userID,
		CreatedByType: pgtype.Text{String: "member", Valid: true},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID: wsID,
		Title:       "Meeting plan",
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	// Real agent — task.primary_assignee_id has FK to agent.id.
	runtime, err := q.EnsureCloudRuntime(ctx, wsID)
	if err != nil {
		t.Fatalf("ensure runtime: %v", err)
	}
	agent, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: wsID,
		Name:        "Assignee " + t.Name(),
		Description: "for meeting test",
		RuntimeID:   runtime.ID,
		OwnerID:     userID,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// --- ASR returns a 2-item summary; one with high confidence ---
	svc := NewMeetingService(q, openTestPool(t), fakeSecrets{}, &fakeASR{
		bundle: asr.SummaryBundle{
			Provider:  "doubao_miaoji",
			Sections:  []string{"intro", "decisions"},
			Decisions: []string{"ship MVP next week"},
			ActionItems: []asr.ActionItem{
				{ID: uuid.NewString(), Task: "Draft PRD", Owner: "Alice", Confidence: 0.95},
				{ID: uuid.NewString(), Task: "Review backend code", Owner: "", Confidence: 0.4},
			},
		},
	}).WithMemory(memory.NewService(q))

	threadID := uuid.UUID(thread.ID.Bytes)

	// 1. StartMeeting flips kind=meeting + status=planned
	meta, err := svc.StartMeeting(ctx, threadID, []string{"intro", "decisions"})
	if err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}
	if meta.Kind != "meeting" || meta.MeetingStatus != MeetingPlanned {
		t.Fatalf("after StartMeeting got kind=%q status=%q", meta.Kind, meta.MeetingStatus)
	}

	// 2. AttachAudio sets audio_file_id + status=transcribing. We don't
	//    actually need a real file_index row here because the service
	//    doesn't dereference it (the Summarize call provides audioURL).
	fakeAudioID := uuid.New()
	meta, err = svc.AttachAudio(ctx, threadID, fakeAudioID)
	if err != nil {
		t.Fatalf("AttachAudio: %v", err)
	}
	if meta.MeetingStatus != MeetingTranscribing || meta.AudioFileID != fakeAudioID.String() {
		t.Fatalf("after AttachAudio status=%q audio=%q", meta.MeetingStatus, meta.AudioFileID)
	}

	// 3. Summarize: ASR returns bundle, service writes summary + 2 action_items
	bundle, err := svc.Summarize(ctx, threadID, "https://example.com/audio.mp3")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(bundle.ActionItems) != 2 {
		t.Fatalf("expected 2 action items, got %d", len(bundle.ActionItems))
	}

	// 4. ListActionItems should return both, both pending
	items, err := svc.ListActionItems(ctx, threadID)
	if err != nil {
		t.Fatalf("ListActionItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items in DB, got %d", len(items))
	}
	for _, it := range items {
		if it.Status != ActionItemPending {
			t.Errorf("item %s should be pending, got %s", it.Task, it.Status)
		}
	}

	// 5. ApproveActionItem on the high-confidence item -> creates Task
	highConf := items[0]
	for _, it := range items {
		if it.Confidence >= 0.9 {
			highConf = it
			break
		}
	}
	// Real agent assignee (Task.primary_assignee_id has FK to agent.id).
	task, err := svc.ApproveActionItem(ctx, highConf.ID, uuid.UUID(plan.ID.Bytes), uuid.UUID(agent.ID.Bytes))
	if err != nil {
		t.Fatalf("ApproveActionItem: %v", err)
	}
	if task.Title != highConf.Task {
		t.Errorf("task title %q != action item task %q", task.Title, highConf.Task)
	}

	// Re-list — the approved one should now have status=approved + task_id
	items, _ = svc.ListActionItems(ctx, threadID)
	approvedFound, pendingFound := 0, 0
	for _, it := range items {
		switch it.Status {
		case ActionItemApproved:
			approvedFound++
			if it.TaskID == "" {
				t.Errorf("approved item %s missing task_id", it.ID)
			}
		case ActionItemPending:
			pendingFound++
		}
	}
	if approvedFound != 1 || pendingFound != 1 {
		t.Errorf("after approve: want 1 approved + 1 pending, got %d/%d", approvedFound, pendingFound)
	}

	// 6. RejectActionItem on the remaining pending -> tombstones it
	for _, it := range items {
		if it.Status == ActionItemPending {
			if err := svc.RejectActionItem(ctx, it.ID); err != nil {
				t.Fatalf("RejectActionItem: %v", err)
			}
			break
		}
	}
	items, _ = svc.ListActionItems(ctx, threadID)
	rejectedFound := 0
	for _, it := range items {
		if it.Status == ActionItemRejected {
			rejectedFound++
		}
	}
	if rejectedFound != 1 {
		t.Errorf("expected 1 rejected, got %d", rejectedFound)
	}

	// Sanity: thread.metadata should now reflect summary + provider
	updated, err := q.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("reload thread: %v", err)
	}
	var stored MeetingMeta
	if err := json.Unmarshal(updated.Metadata, &stored); err != nil {
		t.Fatalf("decode stored metadata: %v", err)
	}
	if stored.MeetingStatus != MeetingSummarized {
		t.Errorf("final status: want %q, got %q", MeetingSummarized, stored.MeetingStatus)
	}
	if stored.ASRProvider != "doubao_miaoji" {
		t.Errorf("provider: want doubao_miaoji, got %q", stored.ASRProvider)
	}

	// Phase 2 dual-write: each action_item should also have a parallel
	// memory_record row pointing at it. Status=candidate, type=task.
	memSvc := memory.NewService(q)
	dbItems := getActionItemRows(t, q, thread.ID)
	totalMem := 0
	for _, it := range dbItems {
		mems, err := memSvc.GetByRaw(ctx, memory.RawRef{
			Kind: memory.RawThreadContextItem,
			ID:   uuid.UUID(it.ID.Bytes),
		})
		if err != nil {
			t.Fatalf("memory.GetByRaw: %v", err)
		}
		for _, m := range mems {
			if m.Type != memory.TypeTask {
				t.Errorf("memory type: want task, got %s", m.Type)
			}
			// Phase S: high-confidence (>= AutoApproveThreshold=0.85)
			// rows are auto-promoted to confirmed; low-confidence stay
			// candidate. Test action items: 0.95 (auto) + 0.4 (candidate).
			if m.Confidence >= AutoApproveThreshold {
				if m.Status != memory.StatusConfirmed {
					t.Errorf("high-conf memory: want confirmed, got %s", m.Status)
				}
			} else {
				if m.Status != memory.StatusCandidate {
					t.Errorf("low-conf memory: want candidate, got %s", m.Status)
				}
			}
			if m.Scope != memory.ScopeSharedSummary {
				t.Errorf("memory scope: want shared_summary, got %s", m.Scope)
			}
			totalMem++
		}
	}
	if totalMem != 2 {
		t.Errorf("expected 2 memory_record rows (one per action_item), got %d", totalMem)
	}
}

// getActionItemRows reads thread_context_item rows directly so the test
// can verify dual-write without depending on the higher-level
// ListActionItems projection.
func getActionItemRows(t *testing.T, q *db.Queries, threadID pgtype.UUID) []db.ThreadContextItem {
	t.Helper()
	items, err := q.ListThreadContextItemsByType(context.Background(), db.ListThreadContextItemsByTypeParams{
		ThreadID: threadID,
		ItemType: "action_item",
	})
	if err != nil {
		t.Fatalf("list context items: %v", err)
	}
	return items
}

func TestMeetingService_NotMeeting(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()
	wsID := createTestWorkspace(t, q)
	userID := createTestUser(t, q, "nm@example.com", "NM")
	if _, err := q.CreateMember(ctx, db.CreateMemberParams{WorkspaceID: wsID, UserID: userID, Role: "owner"}); err != nil {
		t.Fatalf("member: %v", err)
	}
	ch, err := q.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID: wsID, Name: "nm-" + uuid.NewString()[:8],
		CreatedBy: userID, CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	thread, err := q.CreateThread(ctx, db.CreateThreadParams{
		ChannelID:   ch.ID,
		WorkspaceID: wsID,
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	svc := NewMeetingService(q, openTestPool(t), fakeSecrets{}, &fakeASR{})
	// Did NOT call StartMeeting — Summarize must refuse
	_, err = svc.Summarize(ctx, uuid.UUID(thread.ID.Bytes), "u")
	if err == nil || err.Error() != ErrNotMeeting.Error() {
		t.Fatalf("expected ErrNotMeeting, got %v", err)
	}
}

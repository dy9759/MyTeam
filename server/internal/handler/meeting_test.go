package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/asr"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// handlerFakeASR returns a deterministic SummaryBundle so tests don't
// need network or real Feishu credentials.
type handlerFakeASR struct {
	bundle asr.SummaryBundle
	err    error
}

func (f *handlerFakeASR) BatchSummarize(_ context.Context, _ asr.Credentials, _ string) (asr.SummaryBundle, error) {
	return f.bundle, f.err
}

// handlerFakeSecrets satisfies service.SecretGetter without touching
// encrypted workspace_secret rows. Returns canned values for the three
// Feishu keys MeetingService.loadCredentials looks up.
type handlerFakeSecrets struct{}

func (handlerFakeSecrets) GetPlaintext(_ context.Context, _ uuid.UUID, key string) (string, error) {
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

// withMeetingDeps wires Secrets+ASR onto testHandler for the test, then
// restores originals on cleanup. Using fakes avoids network and
// MYTEAM_SECRET_KEY env requirements.
func withMeetingDeps(t *testing.T, ar asr.Client) {
	t.Helper()
	prevSecrets := testHandler.Secrets
	prevASR := testHandler.ASR
	testHandler.Secrets = handlerFakeSecrets{}
	testHandler.ASR = ar
	t.Cleanup(func() {
		testHandler.Secrets = prevSecrets
		testHandler.ASR = prevASR
	})
}

type meetingFixture struct {
	threadID  string
	channelID string
	planID    string
	agentID   string
	cleanup   func()
}

func newMeetingFixture(t *testing.T) meetingFixture {
	t.Helper()
	ctx := context.Background()
	q := testHandler.Queries

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	ch, err := q.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID:   wsUUID,
		Name:          "meeting-handler-" + uuid.NewString()[:8],
		Description:   pgtype.Text{String: "meeting handler test channel", Valid: true},
		CreatedBy:     userUUID,
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	thread, err := q.CreateThread(ctx, db.CreateThreadParams{
		ChannelID:     ch.ID,
		WorkspaceID:   wsUUID,
		Title:         pgtype.Text{String: "Meeting handler test", Valid: true},
		Status:        pgtype.Text{String: "active", Valid: true},
		CreatedBy:     userUUID,
		CreatedByType: pgtype.Text{String: "member", Valid: true},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID: wsUUID,
		Title:       "Meeting handler plan",
		CreatedBy:   userUUID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	// Reuse the agent the global handler-test fixture created — there's a
	// uniq(workspace_id, owner_id) constraint on personal_agent so we
	// can't add another for the same (testWorkspaceID, testUserID) pair.
	var agentRow struct{ ID string }
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentRow.ID); err != nil {
		t.Fatalf("lookup fixture agent: %v", err)
	}

	cleanup := func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM thread_context_item WHERE thread_id = $1`, uuidToString(thread.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM task WHERE plan_id = $1`, uuidToString(plan.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM thread WHERE id = $1`, uuidToString(thread.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM plan WHERE id = $1`, uuidToString(plan.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM channel WHERE id = $1`, uuidToString(ch.ID))
	}

	return meetingFixture{
		threadID:  uuidToString(thread.ID),
		channelID: uuidToString(ch.ID),
		planID:    uuidToString(plan.ID),
		agentID:   agentRow.ID,
		cleanup:   cleanup,
	}
}

// TestMeeting_NotWired — endpoints return 503 when ASR/Secrets missing.
func TestMeeting_NotWired(t *testing.T) {
	fx := newMeetingFixture(t)
	t.Cleanup(fx.cleanup)

	prevASR, prevSec := testHandler.ASR, testHandler.Secrets
	testHandler.ASR = nil
	testHandler.Secrets = nil
	t.Cleanup(func() {
		testHandler.ASR = prevASR
		testHandler.Secrets = prevSec
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/threads/"+fx.threadID+"/meeting/start", nil)
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.StartMeeting(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMeeting_FullHappyPath — Start → Summarize (fake ASR) → List →
// Approve → Reject. Covers each handler end-to-end against real DB.
func TestMeeting_FullHappyPath(t *testing.T) {
	fx := newMeetingFixture(t)
	t.Cleanup(fx.cleanup)

	withMeetingDeps(t, &handlerFakeASR{
		bundle: asr.SummaryBundle{
			Provider:  "fake",
			Sections:  []string{"intro"},
			Decisions: []string{"ship MVP"},
			ActionItems: []asr.ActionItem{
				{ID: uuid.NewString(), Task: "Draft PRD", Owner: "Alice", Confidence: 0.95},
				{ID: uuid.NewString(), Task: "Review backend", Confidence: 0.4},
			},
		},
	})

	// Start.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/threads/"+fx.threadID+"/meeting/start", map[string]any{
		"agenda": []string{"intro", "decisions"},
	})
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.StartMeeting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartMeeting: %d %s", w.Code, w.Body.String())
	}
	var meta service.MeetingMeta
	json.NewDecoder(w.Body).Decode(&meta)
	if meta.Kind != "meeting" || meta.MeetingStatus != service.MeetingPlanned {
		t.Fatalf("after Start: kind=%q status=%q", meta.Kind, meta.MeetingStatus)
	}

	// Summarize with audio_url override (skips Storage path).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/threads/"+fx.threadID+"/meeting/summarize",
		map[string]any{"audio_url": "https://example.com/a.mp3"})
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.SummarizeMeeting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Summarize: %d %s", w.Code, w.Body.String())
	}
	var bundle asr.SummaryBundle
	json.NewDecoder(w.Body).Decode(&bundle)
	if len(bundle.ActionItems) != 2 {
		t.Fatalf("expected 2 action items, got %d", len(bundle.ActionItems))
	}

	// List.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/threads/"+fx.threadID+"/meeting/action-items", nil)
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.ListMeetingActionItems(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListActionItems: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Items []service.ActionItemView `json:"items"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	if len(listResp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(listResp.Items))
	}

	// Approve the high-confidence one. Pick by confidence so test is
	// stable regardless of insertion order.
	var approveItem, rejectItem service.ActionItemView
	for _, it := range listResp.Items {
		if it.Confidence >= 0.9 {
			approveItem = it
		} else {
			rejectItem = it
		}
	}
	if approveItem.ID == uuid.Nil || rejectItem.ID == uuid.Nil {
		t.Fatalf("could not split items by confidence: %+v", listResp.Items)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/action-items/"+approveItem.ID.String()+"/approve",
		map[string]any{"plan_id": fx.planID, "agent_id": fx.agentID})
	req = withURLParam(req, "itemID", approveItem.ID.String())
	testHandler.ApproveActionItem(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Approve: %d %s", w.Code, w.Body.String())
	}

	// Reject the low-confidence one.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/action-items/"+rejectItem.ID.String()+"/reject", nil)
	req = withURLParam(req, "itemID", rejectItem.ID.String())
	testHandler.RejectActionItem(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Reject: %d %s", w.Code, w.Body.String())
	}

	// Re-list — one approved (with task_id), one rejected.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/threads/"+fx.threadID+"/meeting/action-items", nil)
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.ListMeetingActionItems(w, req)
	json.NewDecoder(w.Body).Decode(&listResp)
	approved, rejected := 0, 0
	for _, it := range listResp.Items {
		switch it.Status {
		case service.ActionItemApproved:
			approved++
			if it.TaskID == "" {
				t.Errorf("approved item missing task_id")
			}
		case service.ActionItemRejected:
			rejected++
		}
	}
	if approved != 1 || rejected != 1 {
		t.Errorf("post-approve/reject: want 1/1, got %d/%d", approved, rejected)
	}
}

// TestMeeting_SummarizeBeforeStart — calling Summarize on a regular
// thread (kind != meeting) returns 400, not 500.
func TestMeeting_SummarizeBeforeStart(t *testing.T) {
	fx := newMeetingFixture(t)
	t.Cleanup(fx.cleanup)

	withMeetingDeps(t, &handlerFakeASR{})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/threads/"+fx.threadID+"/meeting/summarize",
		map[string]any{"audio_url": "https://example.com/a.mp3"})
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.SummarizeMeeting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMeeting_ThreadNotFound — invalid thread id → 404.
func TestMeeting_ThreadNotFound(t *testing.T) {
	withMeetingDeps(t, &handlerFakeASR{})

	bogus := "00000000-0000-0000-0000-000000000099"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/threads/"+bogus+"/meeting/start", nil)
	req = withURLParam(req, "threadID", bogus)
	testHandler.StartMeeting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMeeting_RejectMissingItem — bad item id → 404.
func TestMeeting_RejectMissingItem(t *testing.T) {
	withMeetingDeps(t, &handlerFakeASR{})

	bogus := "00000000-0000-0000-0000-000000000098"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/action-items/"+bogus+"/reject", nil)
	req = withURLParam(req, "itemID", bogus)
	testHandler.RejectActionItem(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

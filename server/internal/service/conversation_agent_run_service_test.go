package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func conversationRunTestDB(t *testing.T) (*db.Queries, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("db pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return db.New(pool), pool
}

func createLocalRuntimeAgentAndDM(t *testing.T, q *db.Queries, pool *pgxpool.Pool) (pgtype.UUID, pgtype.UUID, db.AgentRuntime, db.Agent, db.Message) {
	t.Helper()
	ctx := context.Background()

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "local-dm+"+t.Name()+"@example.com", "Local DM User")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})

	runtime, err := q.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
		WorkspaceID: wsID,
		DaemonID:    pgtype.Text{String: "daemon-" + strings.ReplaceAll(t.Name(), "/", "-"), Valid: true},
		Name:        "Local Claude",
		Mode:        pgtype.Text{String: "local", Valid: true},
		Provider:    "claude",
		Status:      "online",
		DeviceInfo:  "test daemon",
		Metadata:    []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create local runtime: %v", err)
	}

	agent, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: wsID,
		Name:        "Local Bot",
		Description: "test local bot",
		RuntimeID:   runtime.ID,
		OwnerID:     ownerID,
	})
	if err != nil {
		t.Fatalf("create local agent: %v", err)
	}

	trigger, err := q.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID:   wsID,
		SenderID:      ownerID,
		SenderType:    "member",
		RecipientID:   agent.ID,
		RecipientType: util.StrToText("agent"),
		Content:       "hello from a DM",
		ContentType:   "text",
		Type:          "user",
	})
	if err != nil {
		t.Fatalf("create trigger dm: %v", err)
	}

	return wsID, ownerID, runtime, agent, trigger
}

func TestReplyToDM_LocalRuntimeEnqueuesRunAndDoesNotCallCloudRunner(t *testing.T) {
	q, pool := conversationRunTestDB(t)
	wsID, ownerID, runtime, agent, trigger := createLocalRuntimeAgentAndDM(t, q, pool)

	runner := &fakeRunner{reply: "cloud should not run"}
	runs := NewConversationAgentRunService(q, pool, nil)
	svc := NewAutoReplyService(q, nil, runner, runs)

	svc.ReplyToDM(context.Background(), uuidToStr(agent.ID), uuidToStr(wsID), uuidToStr(ownerID), trigger)

	if runner.lastPrompt != "" {
		t.Fatalf("local runtime should enqueue instead of calling cloud runner, got prompt %q", runner.lastPrompt)
	}

	claimed, err := runs.ClaimNext(context.Background(), uuidToStr(runtime.ID))
	if err != nil {
		t.Fatalf("claim enqueued run: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected local DM to enqueue a conversation agent run")
	}
	if claimed.TriggerMessageID != uuidToStr(trigger.ID) {
		t.Fatalf("trigger id mismatch: got %s want %s", claimed.TriggerMessageID, uuidToStr(trigger.ID))
	}
	if !strings.Contains(claimed.Prompt, "hello from a DM") {
		t.Fatalf("prompt should include DM history, got %q", claimed.Prompt)
	}
}

func TestConversationAgentRunService_ClaimsOnceAndStreamsDMReply(t *testing.T) {
	q, pool := conversationRunTestDB(t)
	wsID, ownerID, runtime, agent, trigger := createLocalRuntimeAgentAndDM(t, q, pool)
	runs := NewConversationAgentRunService(q, pool, nil)

	run, err := runs.EnqueueDMRun(context.Background(), ConversationAgentRunInput{
		WorkspaceID:      uuidToStr(wsID),
		TriggerMessageID: uuidToStr(trigger.ID),
		AgentID:          uuidToStr(agent.ID),
		RuntimeID:        uuidToStr(runtime.ID),
		PeerUserID:       uuidToStr(ownerID),
		Provider:         runtime.Provider,
		Prompt:           "reply to the DM",
	})
	if err != nil {
		t.Fatalf("enqueue run: %v", err)
	}
	if run.Status != "queued" {
		t.Fatalf("new run status: got %s want queued", run.Status)
	}

	claimed, err := runs.ClaimNext(context.Background(), uuidToStr(runtime.ID))
	if err != nil {
		t.Fatalf("claim run: %v", err)
	}
	if claimed == nil || claimed.ID != run.ID || claimed.Status != "claimed" {
		t.Fatalf("claim returned wrong run: %+v", claimed)
	}
	claimedAgain, err := runs.ClaimNext(context.Background(), uuidToStr(runtime.ID))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimedAgain != nil {
		t.Fatalf("run should only be claimed once, got %+v", claimedAgain)
	}

	if err := runs.MarkRunning(context.Background(), run.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := runs.AppendEvents(context.Background(), run.ID, []ConversationAgentRunEventInput{
		{Seq: 1, Type: "text", Content: "hello "},
		{Seq: 2, Type: "tool_use", Tool: "read_file", Input: map[string]any{"path": "README.md"}},
		{Seq: 3, Type: "assistant_delta", Content: "world"},
	}); err != nil {
		t.Fatalf("append events: %v", err)
	}

	streamingRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload streaming run: %v", err)
	}
	if streamingRun.ResponseMessageID == "" {
		t.Fatal("streaming text should create a response DM message")
	}
	if streamingRun.Output != "hello world" {
		t.Fatalf("streamed output mismatch: got %q", streamingRun.Output)
	}

	msg, err := q.GetMessage(context.Background(), util.ParseUUID(streamingRun.ResponseMessageID))
	if err != nil {
		t.Fatalf("load response message: %v", err)
	}
	if msg.Content != "hello world" {
		t.Fatalf("response message content: got %q", msg.Content)
	}

	if err := runs.Complete(context.Background(), run.ID, "", "session-123", "/tmp/local-run"); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	completed, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload completed run: %v", err)
	}
	if completed.Status != "completed" || completed.SessionID != "session-123" || completed.WorkDir != "/tmp/local-run" {
		t.Fatalf("completed run fields wrong: %+v", completed)
	}

	msg, err = q.GetMessage(context.Background(), util.ParseUUID(streamingRun.ResponseMessageID))
	if err != nil {
		t.Fatalf("reload response message: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(msg.Metadata, &metadata); err != nil {
		t.Fatalf("decode response metadata: %v", err)
	}
	if metadata["source"] != "local_agent" || metadata["streaming"] != false {
		t.Fatalf("response metadata should mark completed local stream, got %+v", metadata)
	}
}

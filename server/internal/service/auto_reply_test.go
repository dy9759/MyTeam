package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent_runner"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeRunner stubs the Python child for tests.
type fakeRunner struct {
	reply      string
	err        error
	lastCfg    agent_runner.Config
	lastPrompt string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string, cfg agent_runner.Config) (string, error) {
	f.lastCfg = cfg
	f.lastPrompt = prompt
	return f.reply, f.err
}

// insertTestAgent inserts a personal-agent row and writes the supplied cloud
// LLM config under runtime.metadata.cloud_llm_config (post Account Phase 2).
func insertTestAgent(t *testing.T, q *db.Queries, wsID, runtimeID, ownerID pgtype.UUID, name string, cfg CloudLLMConfig) db.Agent {
	t.Helper()
	cfgJSON, _ := json.Marshal(cfg)
	a, err := q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID: wsID,
		Name:        name,
		Description: "test agent",
		RuntimeID:   runtimeID,
		OwnerID:     ownerID,
	})
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if err := q.SetRuntimeMetadataKey(context.Background(), db.SetRuntimeMetadataKeyParams{
		ID:    runtimeID,
		Key:   "cloud_llm_config",
		Value: cfgJSON,
	}); err != nil {
		t.Fatalf("seed runtime cloud_llm_config: %v", err)
	}
	return a
}

func createTestChannel(t *testing.T, q *db.Queries, wsID pgtype.UUID, creatorID pgtype.UUID) db.Channel {
	t.Helper()
	ch, err := q.CreateChannel(context.Background(), db.CreateChannelParams{
		WorkspaceID:   wsID,
		Name:          "test-channel-" + strings.ReplaceAll(t.Name(), "/", "-"),
		Description:   pgtype.Text{},
		CreatedBy:     creatorID,
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

func createTestMessage(t *testing.T, q *db.Queries, wsID, senderID, channelID pgtype.UUID, content string) db.Message {
	t.Helper()
	m, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: wsID,
		SenderID:    senderID,
		SenderType:  "member",
		ChannelID:   channelID,
		Content:     content,
		ContentType: "text",
		Type:        "user",
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	return m
}

func uuidToStr(u pgtype.UUID) string {
	return util.UUIDToString(u)
}

func TestReplyAsMentionedAgent_DispatchesToRunner(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t1+"+t.Name()+"@x.com", "T1")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)

	cfg := CloudLLMConfig{Kernel: "openai_compat", BaseURL: "http://b", APIKey: "sk-X", Model: "qwen-plus"}
	_ = insertTestAgent(t, q, wsID, runtime.ID, ownerID, "Bot", cfg)
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hello")

	runner := &fakeRunner{reply: "hi there"}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	if err := svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if runner.lastCfg.APIKey != "sk-X" {
		t.Fatalf("runner did not receive agent config: %+v", runner.lastCfg)
	}

	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if m.Content == "hi there" {
			found = true
			if m.SenderType != "agent" {
				t.Fatalf("sender_type expected 'agent', got %q", m.SenderType)
			}
		}
	}
	if !found {
		t.Fatal("reply message not inserted")
	}
}

func TestReplyAsMentionedAgent_NoAPIKey_PostsSystemNotification(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t2+"+t.Name()+"@x.com", "T2")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)
	_ = insertTestAgent(t, q, wsID, runtime.ID, ownerID, "Bot", CloudLLMConfig{Kernel: "openai_compat", APIKey: ""})
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if runner.lastPrompt != "" {
		t.Fatal("runner should not be called when api key missing")
	}
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "not configured") {
			found = true
		}
	}
	if !found {
		t.Fatal("system_notification not posted")
	}
}

func TestReplyAsMentionedAgent_RunnerError_PostsSystemNotification(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t3+"+t.Name()+"@x.com", "T3")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)
	cfg := CloudLLMConfig{Kernel: "openai_compat", APIKey: "sk-X", Model: "m"}
	_ = insertTestAgent(t, q, wsID, runtime.ID, ownerID, "Bot", cfg)
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hi")

	runner := &fakeRunner{err: errors.New("boom sk-abcdefgh")}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "Agent reply failed") {
			found = true
			if strings.Contains(m.Content, "sk-abcdefgh") {
				t.Fatal("api key not redacted in error message")
			}
		}
	}
	if !found {
		t.Fatal("error system_notification not posted")
	}
}

func TestReplyAsMentionedAgent_AgentNotFound_Silent(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t4+"+t.Name()+"@x.com", "T4")
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@GhostBot hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	err := svc.replyAsMentionedAgent(context.Background(), "GhostBot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if err != nil {
		t.Fatalf("agent-not-found should be silent (nil err), got: %v", err)
	}
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (trigger only), got %d", len(msgs))
	}
}

func TestReplyAsMentionedAgent_OnMentionDisabled_Silent(t *testing.T) {
	// Trigger-level eligibility (per-agent on_mention enable/disable) was
	// removed in Account Phase 2. The stored auto_reply_config now uses a
	// different shape and gating moves to MediationService in a later phase.
	// Test kept as a placeholder so the file structure stays familiar.
	t.Skip("on_mention trigger flag removed in Account Phase 2; gating moves to MediationService in Plan 4")
}

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

// insertTestAgent inserts a personal-agent row.
func insertTestAgent(t *testing.T, q *db.Queries, wsID, runtimeID, ownerID pgtype.UUID, name string, cfg CloudLLMConfig) db.Agent {
	t.Helper()
	cfgJSON, _ := json.Marshal(cfg)
	triggers, _ := json.Marshal([]map[string]any{{"type": "on_mention", "enabled": true}})
	a, err := q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID:    wsID,
		Name:           name,
		Description:    "test agent",
		RuntimeID:      runtimeID,
		OwnerID:        ownerID,
		CloudLlmConfig: cfgJSON,
		Triggers:       triggers,
	})
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	// Bring agent online so auto-reply's "agent offline → notify owner and stop"
	// early-return branch doesn't fire in tests that exercise the post-eligibility path.
	if err := q.UpdateAgentOnlineStatus(context.Background(), db.UpdateAgentOnlineStatusParams{
		ID:           a.ID,
		OnlineStatus: "online",
	}); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	a.OnlineStatus = "online"
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
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t5+"+t.Name()+"@x.com", "T5")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)

	cfgJSON, _ := json.Marshal(CloudLLMConfig{APIKey: "sk-X", Kernel: "openai_compat", Model: "m"})
	triggers, _ := json.Marshal([]map[string]any{{"type": "on_mention", "enabled": false}})
	_, err := q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID:    wsID,
		Name:           "Muted",
		Description:    "test",
		RuntimeID:      runtime.ID,
		OwnerID:        ownerID,
		CloudLlmConfig: cfgJSON,
		Triggers:       triggers,
	})
	if err != nil {
		t.Fatalf("create muted agent: %v", err)
	}
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Muted hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Muted", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if runner.lastPrompt != "" {
		t.Fatal("runner should not run when on_mention disabled")
	}
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected only trigger message, got %d", len(msgs))
	}
}

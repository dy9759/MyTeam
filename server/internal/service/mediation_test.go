package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var medRandCounter uint64

// mediationFixture wires a MediationService against the real test database.
// Pool is needed because some mediation ops hit SQL directly.
type mediationFixture struct {
	t      *testing.T
	q      *db.Queries
	pool   *pgxpool.Pool
	svc    *MediationService
	wsID   pgtype.UUID
	userID pgtype.UUID
}

func newMediationFixture(t *testing.T) *mediationFixture {
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

	q := db.New(pool)
	wsID := createTestWorkspace(t, q)
	userID := createTestUser(t, q, "med+"+t.Name()+"@x.com", "Med User")

	svc := &MediationService{
		Queries: q,
		DB:      pool,
	}
	return &mediationFixture{t: t, q: q, pool: pool, svc: svc, wsID: wsID, userID: userID}
}

// insertMediationAgent creates a personal agent with a runtime. Each
// call gets a fresh owner since migration 062 enforces one personal_agent
// per (workspace_id, owner_id).
func (f *mediationFixture) insertAgent(name string) db.Agent {
	f.t.Helper()
	rt, err := f.q.EnsureCloudRuntime(context.Background(), f.wsID)
	if err != nil {
		f.t.Fatalf("ensure runtime: %v", err)
	}
	owner := createTestUser(f.t, f.q, "mediation-"+name+"@example.com", "Mediation Owner "+name)
	a, err := f.q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID: f.wsID,
		Name:        name,
		Description: "test agent",
		RuntimeID:   rt.ID,
		OwnerID:     owner,
	})
	if err != nil {
		f.t.Fatalf("create agent: %v", err)
	}
	return a
}

// newChannelWithAgent adds the agent as a channel member.
func (f *mediationFixture) newChannel(agents ...db.Agent) db.Channel {
	f.t.Helper()
	ch, err := f.q.CreateChannel(context.Background(), db.CreateChannelParams{
		WorkspaceID:   f.wsID,
		Name:          "m-" + randStr(),
		Description:   pgtype.Text{},
		CreatedBy:     f.userID,
		CreatedByType: "member",
	})
	if err != nil {
		f.t.Fatalf("create channel: %v", err)
	}
	// Add the creating user so owner lookups work.
	_ = f.q.AddChannelMember(context.Background(), db.AddChannelMemberParams{
		ChannelID:  ch.ID,
		MemberID:   f.userID,
		MemberType: "member",
	})
	for _, a := range agents {
		if err := f.q.AddChannelMember(context.Background(), db.AddChannelMemberParams{
			ChannelID:  ch.ID,
			MemberID:   a.ID,
			MemberType: "agent",
		}); err != nil {
			f.t.Fatalf("add channel member: %v", err)
		}
	}
	return ch
}

func randStr() string {
	// Atomic counter + nanosecond timestamp keeps it unique across rapid calls.
	n := atomic.AddUint64(&medRandCounter, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}

// newMessage inserts a member message into the channel.
func (f *mediationFixture) newMemberMessage(ch db.Channel, content string) db.Message {
	f.t.Helper()
	m, err := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     content,
		ContentType: "text",
		Type:        "user",
	})
	if err != nil {
		f.t.Fatalf("create message: %v", err)
	}
	return m
}

// newAgentMessage inserts an agent message in a thread/channel.
func (f *mediationFixture) newAgentMessage(ch db.Channel, threadID pgtype.UUID, a db.Agent, content string) db.Message {
	f.t.Helper()
	m, err := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    a.ID,
		SenderType:  "agent",
		ChannelID:   ch.ID,
		Content:     content,
		ContentType: "text",
		Type:        "agent_reply",
	})
	if err != nil {
		f.t.Fatalf("create agent message: %v", err)
	}
	// Attach to thread by updating thread_id directly (no public query yet).
	if threadID.Valid {
		_, _ = f.pool.Exec(context.Background(),
			`UPDATE message SET thread_id = $1 WHERE id = $2`, threadID, m.ID)
		m.ThreadID = threadID
	}
	return m
}

// newThread creates a thread rooted at a message (optionally with an issue).
func (f *mediationFixture) newThread(ch db.Channel, root db.Message, issueID pgtype.UUID) db.Thread {
	f.t.Helper()
	rootID := pgtype.UUID{Bytes: root.ID.Bytes, Valid: true}
	params := db.CreateThreadParams{
		ChannelID:     ch.ID,
		WorkspaceID:   f.wsID,
		RootMessageID: rootID,
		IssueID:       issueID,
		Status:        pgtype.Text{String: "active", Valid: true},
	}
	th, err := f.q.CreateThread(context.Background(), params)
	if err != nil {
		f.t.Fatalf("create thread: %v", err)
	}
	// Point the root message's thread_id back to the thread for proper lookups.
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE message SET thread_id = $1 WHERE id = $2`, th.ID, root.ID)
	return th
}

// newIssueWithAssignee creates an issue with an agent assignee.
func (f *mediationFixture) newIssueWithAssignee(assignee db.Agent) db.Issue {
	f.t.Helper()
	issue, err := f.q.CreateIssue(context.Background(), db.CreateIssueParams{
		WorkspaceID:  f.wsID,
		Title:        "Test issue " + randStr(),
		Description:  pgtype.Text{},
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   assignee.ID,
		CreatorType:  "member",
		CreatorID:    f.userID,
		Position:     1.0,
	})
	if err != nil {
		f.t.Fatalf("create issue: %v", err)
	}
	return issue
}

// -------------------- Routing tests --------------------

func TestMediation_Routing_MentionWins(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("AgentA_" + randStr())
	agentB := f.insertAgent("AgentB_" + randStr())
	// Plan/issue route would want agentA, but @AgentB wins.
	ch := f.newChannel(agentA, agentB)
	root := f.newMemberMessage(ch, "root")
	issue := f.newIssueWithAssignee(agentA)
	thread := f.newThread(ch, root, issue.ID)

	// New message mentions AgentB.
	msg, err := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     "hey @" + agentB.Name + " ping",
		ContentType: "text",
		Type:        "user",
	})
	if err != nil {
		t.Fatalf("create msg: %v", err)
	}
	// Set thread_id so plan/issue branch *would* trigger if mention didn't win.
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE message SET thread_id = $1 WHERE id = $2`, thread.ID, msg.ID)
	msg.ThreadID = thread.ID

	decision, err := f.svc.RouteMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision == nil || decision.Agent == nil {
		t.Fatalf("expected a decision with agent")
	}
	if decision.Reason != "mention" {
		t.Fatalf("expected reason=mention, got %q", decision.Reason)
	}
	if util.UUIDToString(decision.Agent.ID) != util.UUIDToString(agentB.ID) {
		t.Fatalf("expected agentB, got %s", decision.Agent.Name)
	}
}

func TestMediation_Routing_PlanThreadFallback(t *testing.T) {
	// When the thread is not bound to a plan (no row in plan with thread_id =
	// thread.ID), findPlanForThread returns nil and routing falls through to
	// the issue/capability branch. This test exercises that fallback.
	f := newMediationFixture(t)
	agentA := f.insertAgent("PlanFallbackA_" + randStr())
	ch := f.newChannel(agentA)
	root := f.newMemberMessage(ch, "root")
	thread := f.newThread(ch, root, pgtype.UUID{}) // no issue, no plan

	msg, _ := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     "plain message",
		ContentType: "text",
		Type:        "user",
	})
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE message SET thread_id = $1 WHERE id = $2`, thread.ID, msg.ID)
	msg.ThreadID = thread.ID

	decision, err := f.svc.RouteMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision == nil || decision.Agent == nil {
		t.Fatalf("expected a decision (capability fallback)")
	}
	// With no plan and no issue, capability should match agentA.
	if decision.Reason != "capability" {
		t.Fatalf("expected reason=capability, got %q", decision.Reason)
	}
	if util.UUIDToString(decision.Agent.ID) != util.UUIDToString(agentA.ID) {
		t.Fatalf("expected agentA, got %s", decision.Agent.Name)
	}
}

func TestMediation_Routing_IssueThreadFallback(t *testing.T) {
	f := newMediationFixture(t)
	assignee := f.insertAgent("IssueAssignee_" + randStr())
	someoneElse := f.insertAgent("OtherAgent_" + randStr())
	ch := f.newChannel(assignee, someoneElse)
	root := f.newMemberMessage(ch, "root")
	issue := f.newIssueWithAssignee(assignee)
	thread := f.newThread(ch, root, issue.ID)

	// No @ mention, thread has an issue linked.
	msg, _ := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     "what's the status?",
		ContentType: "text",
		Type:        "user",
	})
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE message SET thread_id = $1 WHERE id = $2`, thread.ID, msg.ID)
	msg.ThreadID = thread.ID

	decision, err := f.svc.RouteMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision == nil || decision.Agent == nil {
		t.Fatalf("expected decision, got nil")
	}
	if decision.Reason != "issue_assignee" {
		t.Fatalf("expected reason=issue_assignee, got %q", decision.Reason)
	}
	if util.UUIDToString(decision.Agent.ID) != util.UUIDToString(assignee.ID) {
		t.Fatalf("expected issue assignee, got %s", decision.Agent.Name)
	}
}

func TestMediation_Routing_CapabilityFallback(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("CapA_" + randStr())
	agentB := f.insertAgent("CapB_" + randStr())
	// Note: agentA is inserted first → will be returned by ListChannelMembers
	// (which orders by join time).
	_ = agentB
	ch := f.newChannel(agentA, agentB)

	// No thread, no mention, no issue.
	msg, _ := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     "help please",
		ContentType: "text",
		Type:        "user",
	})

	decision, err := f.svc.RouteMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision == nil || decision.Agent == nil {
		t.Fatalf("expected capability fallback agent")
	}
	if decision.Reason != "capability" {
		t.Fatalf("expected reason=capability, got %q", decision.Reason)
	}
	// Should pick the first agent member — expect agentA.
	if util.UUIDToString(decision.Agent.ID) != util.UUIDToString(agentA.ID) {
		t.Fatalf("expected agentA, got %s", decision.Agent.Name)
	}
}

// -------------------- Anti-loop tests --------------------

func TestMediation_AntiLoop_SelfReplyBlocked(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("Self_" + randStr())
	ch := f.newChannel(agentA)
	root := f.newMemberMessage(ch, "root")
	thread := f.newThread(ch, root, pgtype.UUID{})

	// Agent-authored message.
	agentMsg := f.newAgentMessage(ch, thread.ID, agentA, "I will look into it")

	err := f.svc.checkAntiLoop(context.Background(), agentMsg, agentA.ID)
	if err == nil {
		t.Fatal("expected self-reply to be blocked, got nil")
	}
}

func TestMediation_AntiLoop_AgentChainLimit(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("ChainA_" + randStr())
	agentB := f.insertAgent("ChainB_" + randStr())
	ch := f.newChannel(agentA, agentB)
	root := f.newMemberMessage(ch, "root")
	thread := f.newThread(ch, root, pgtype.UUID{})

	// 3 consecutive agent messages in the thread.
	f.newAgentMessage(ch, thread.ID, agentA, "first")
	f.newAgentMessage(ch, thread.ID, agentB, "second")
	lastAgentMsg := f.newAgentMessage(ch, thread.ID, agentA, "third")

	// Attempting another agent→agent reply should be blocked.
	err := f.svc.checkAntiLoop(context.Background(), lastAgentMsg, agentB.ID)
	if err == nil {
		t.Fatal("expected agent-chain limit to block 4th consecutive agent reply")
	}
}

func TestMediation_AntiLoop_NonAgentChainIsOK(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("OkA_" + randStr())
	agentB := f.insertAgent("OkB_" + randStr())
	ch := f.newChannel(agentA, agentB)
	root := f.newMemberMessage(ch, "root")
	thread := f.newThread(ch, root, pgtype.UUID{})

	// 2 agent messages, then a member message, then a new member message that
	// should be replyable again because the chain was broken.
	f.newAgentMessage(ch, thread.ID, agentA, "first")
	f.newAgentMessage(ch, thread.ID, agentB, "second")
	// Insert a member message to break the chain.
	memberMsg, _ := f.q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: f.wsID,
		SenderID:    f.userID,
		SenderType:  "member",
		ChannelID:   ch.ID,
		Content:     "new question",
		ContentType: "text",
		Type:        "user",
	})
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE message SET thread_id = $1 WHERE id = $2`, thread.ID, memberMsg.ID)
	memberMsg.ThreadID = thread.ID

	err := f.svc.checkAntiLoop(context.Background(), memberMsg, agentA.ID)
	if err != nil {
		t.Fatalf("expected reply after member breaks the chain, got err: %v", err)
	}
}

// -------------------- SLA tier tests --------------------

func TestMediation_SLA_TierClassifier(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		created time.Time
		want    SLAState
	}{
		{"fresh", now.Add(-30 * time.Second), SLAFresh},
		{"fallback", now.Add(-301 * time.Second), SLAFallbackAssigned},
		{"warning", now.Add(-601 * time.Second), SLAWarning},
		{"critical", now.Add(-901 * time.Second), SLACritical},
	}
	for _, c := range cases {
		slot := db.ReplySlot{
			CreatedAt: pgtype.Timestamptz{Time: c.created, Valid: true},
		}
		got := slaTier(slot)
		if got != c.want {
			t.Fatalf("%s: want %s, got %s", c.name, c.want, got)
		}
	}
}

func TestMediation_SLA_TierActions(t *testing.T) {
	f := newMediationFixture(t)
	agentA := f.insertAgent("SLA_A_" + randStr())
	// Ensure a system agent exists for postSystemNotice.
	_, _ = f.q.CreateSystemAgent(context.Background(), db.CreateSystemAgentParams{
		WorkspaceID: f.wsID,
	})
	ch := f.newChannel(agentA)
	member := f.newMemberMessage(ch, "need help")

	// Insert a reply slot with created_at = 4 minutes ago → Fresh (below 300s).
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO reply_slot (message_id, channel_id, workspace_id, slot_index, content_summary, status, created_at, expires_at)
		VALUES ($1, $2, $3, 0, 'initial', 'pending', NOW() - INTERVAL '4 minutes', NOW() + INTERVAL '1 second')
	`, member.ID, ch.ID, f.wsID)
	if err != nil {
		t.Fatalf("insert slot: %v", err)
	}

	// Fresh path: processSLATiers only picks slots past 300s, so nothing happens.
	f.svc.processSLATiers(context.Background())
	var summary pgtype.Text
	_ = f.pool.QueryRow(context.Background(), `SELECT content_summary FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&summary)
	if summary.Valid && (strings.Contains(summary.String, "[sla:") || strings.Contains(summary.String, "fallback")) {
		t.Fatalf("fresh slot should not be marked: got %q", summary.String)
	}

	// Advance the slot to the fallback tier (>300s).
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE reply_slot SET created_at = NOW() - INTERVAL '350 seconds' WHERE message_id = $1`, member.ID)
	f.svc.processSLATiers(context.Background())

	_ = f.pool.QueryRow(context.Background(), `SELECT content_summary FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&summary)
	if !summary.Valid || !strings.Contains(summary.String, "[sla:fallback]") {
		t.Fatalf("expected fallback tag, got summary=%q", summary.String)
	}
	// Assigned agent_id should be set after fallback.
	var assigned pgtype.UUID
	_ = f.pool.QueryRow(context.Background(),
		`SELECT assigned_agent_id FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&assigned)
	if !assigned.Valid {
		t.Fatalf("expected assigned_agent_id set after fallback tier")
	}

	// Advance to warning (>600s).
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE reply_slot SET created_at = NOW() - INTERVAL '650 seconds' WHERE message_id = $1`, member.ID)
	f.svc.processSLATiers(context.Background())

	_ = f.pool.QueryRow(context.Background(), `SELECT content_summary FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&summary)
	if !strings.Contains(summary.String, "[sla:warning]") {
		t.Fatalf("expected warning tag, got summary=%q", summary.String)
	}
	// Warning inbox_item must exist.
	if !inboxHasTier(t, f.pool, f.wsID, "warning") {
		t.Fatal("expected warning inbox_item")
	}

	// Advance to critical (>900s).
	_, _ = f.pool.Exec(context.Background(),
		`UPDATE reply_slot SET created_at = NOW() - INTERVAL '950 seconds' WHERE message_id = $1`, member.ID)
	f.svc.processSLATiers(context.Background())

	_ = f.pool.QueryRow(context.Background(), `SELECT content_summary FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&summary)
	if !strings.Contains(summary.String, "[sla:critical]") {
		t.Fatalf("expected critical tag, got summary=%q", summary.String)
	}
	if !inboxHasTier(t, f.pool, f.wsID, "critical") {
		t.Fatal("expected critical inbox_item")
	}
	// Critical inbox_item must populate the slot_id column with the originating
	// reply_slot id (Plan 5 wiring).
	var inboxSlotID pgtype.UUID
	err = f.pool.QueryRow(context.Background(), `
		SELECT slot_id FROM inbox_item
		WHERE workspace_id = $1 AND type = 'reply_slow' AND severity = 'action_required'
		ORDER BY created_at DESC LIMIT 1
	`, f.wsID).Scan(&inboxSlotID)
	if err != nil {
		t.Fatalf("query critical inbox slot_id: %v", err)
	}
	if !inboxSlotID.Valid {
		t.Fatal("expected critical inbox_item.slot_id to be set")
	}
	var replySlotID pgtype.UUID
	_ = f.pool.QueryRow(context.Background(),
		`SELECT id FROM reply_slot WHERE message_id = $1`, member.ID).Scan(&replySlotID)
	if util.UUIDToString(inboxSlotID) != util.UUIDToString(replySlotID) {
		t.Fatalf("expected inbox slot_id %s to match reply_slot id %s",
			util.UUIDToString(inboxSlotID), util.UUIDToString(replySlotID))
	}
}

// inboxHasTier checks whether an inbox_item exists for the workspace tagged with
// the given sla_tier in its details JSON.
func inboxHasTier(t *testing.T, pool *pgxpool.Pool, wsID pgtype.UUID, tier string) bool {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT details FROM inbox_item WHERE workspace_id = $1 AND type = 'reply_slow'`, wsID)
	if err != nil {
		t.Fatalf("inbox lookup: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			if v, ok := m["sla_tier"].(string); ok && v == tier {
				return true
			}
		}
	}
	return false
}


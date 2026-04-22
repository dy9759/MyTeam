package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// SLA tier durations. After a reply slot reaches each cutoff with no human/agent
// response, MediationService takes the tier-specific action.
const (
	slaFallbackAfter = 300 * time.Second // T+300: pick a fallback agent.
	slaWarningAfter  = 600 * time.Second // T+600: warning inbox to owner.
	slaCriticalAfter = 900 * time.Second // T+900: critical inbox to owner.

	// mediationMessageTimeout bounds per-message event handling, including any
	// downstream auto-reply work started from the subscriber callback.
	mediationMessageTimeout = 2 * time.Minute

	// mediationSLATickTimeout bounds one SLA scan / escalation pass.
	mediationSLATickTimeout = 30 * time.Second

	// agentChainLimit caps the number of consecutive agent->agent replies in
	// a thread before MediationService refuses further auto-responses.
	agentChainLimit = 3

	// dailyAutoReplyCap is the per-thread / per-agent rolling 24h cap.
	dailyAutoReplyCap = 50

	// initialReplySlotExpiry sets when freshly-created reply slots transition
	// from "pending" to expired. Concrete tier work then takes over via the
	// classifier, but the legacy expiry column still drives the SQL filter.
	initialReplySlotExpiry = "30 seconds"
)

// SLAState classifies a reply slot's lifecycle based on age.
type SLAState int

const (
	// SLAFresh: T+0 to T+300, no action needed yet.
	SLAFresh SLAState = iota
	// SLAFallbackAssigned: T+300 to T+600, primary did not respond — assign fallback agent.
	SLAFallbackAssigned
	// SLAWarning: T+600 to T+900, escalate as warning to owner inbox.
	SLAWarning
	// SLACritical: T+900+, escalate as critical to owner inbox.
	SLACritical
)

func (s SLAState) String() string {
	switch s {
	case SLAFresh:
		return "fresh"
	case SLAFallbackAssigned:
		return "fallback"
	case SLAWarning:
		return "warning"
	case SLACritical:
		return "critical"
	default:
		return "unknown"
	}
}

// RoutingDecision describes how MediationService chose to handle a message.
type RoutingDecision struct {
	// Reason explains which routing branch matched ("mention", "plan",
	// "issue_assignee", "capability", "skip"). Used for logging.
	Reason string

	// Agent is the agent selected to respond. May be nil when no agent is
	// available or routing was skipped (system / agent message, etc).
	Agent *db.Agent

	// Mentioned holds any extracted @mention names (used by AutoReply).
	Mentioned []string

	// SkipReason explains why no routing was performed (e.g. "system_message").
	SkipReason string
}

// MediationService is the single source of message routing for channel/thread
// messages. It owns:
//   - Routing priority: @mention > Plan thread > Issue thread > capability match.
//   - Anti-loop rules: self-reply blocked, agent-chain ≤3, ≤50 auto-replies/24h.
//   - Reply-slot tracking: insert pending slot on member messages, mark replied
//     on agent messages, escalate stalls through 4 SLA tiers.
//
// auto_reply_config on the agent is consulted only from inside MediationService
// (or AutoReplyService when invoked through MediationService). It is no longer
// a direct trigger for replies.
type MediationService struct {
	Queries   *db.Queries
	Hub       *realtime.Hub
	EventBus  *events.Bus
	AutoReply *AutoReplyService
	DB        *pgxpool.Pool

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
}

// NewMediationService creates a new MediationService.
func NewMediationService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, autoReply *AutoReplyService, pool *pgxpool.Pool) *MediationService {
	ctx, cancel := context.WithCancel(context.Background())
	return &MediationService{
		Queries:         q,
		Hub:             hub,
		EventBus:        bus,
		AutoReply:       autoReply,
		DB:              pool,
		lifecycleCtx:    ctx,
		lifecycleCancel: cancel,
	}
}

func (s *MediationService) rootContext() context.Context {
	if s == nil || s.lifecycleCtx == nil {
		return context.Background()
	}
	return s.lifecycleCtx
}

func mediationTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

// Start subscribes to message:created events and runs mediation logic.
func (s *MediationService) Start() {
	if s.lifecycleCtx == nil {
		s.lifecycleCtx, s.lifecycleCancel = context.WithCancel(context.Background())
	}
	ctx := s.rootContext()
	s.EventBus.Subscribe(protocol.EventMessageCreated, func(e events.Event) {
		go s.handleMessageCreated(e)
	})
	// Tier-classifier loop: every 10 seconds, scan reply slots and act on each tier.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tickCtx, cancel := mediationTimeoutContext(ctx, mediationSLATickTimeout)
				s.processSLATiers(tickCtx)
				cancel()
			}
		}
	}()
	slog.Info("[mediation] service started, listening for message:created events")
}

// handleMessageCreated processes a new message: tracks reply slots, then runs
// the unified routing decision through RouteMessage.
func (s *MediationService) handleMessageCreated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	msgData, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}

	msg := messageFromMap(msgData, e.WorkspaceID)
	if !msg.ChannelID.Valid {
		return // Not a channel message — skip mediation entirely.
	}

	ctx, cancel := mediationTimeoutContext(s.rootContext(), mediationMessageTimeout)
	defer cancel()

	// Track reply slots based on sender type.
	s.trackReplySlot(ctx, msg)

	// Don't route system/agent messages — they are responses, not triggers.
	if msg.SenderType == "agent" || msg.SenderType == "system" {
		return
	}

	// Run unified routing.
	decision, err := s.RouteMessage(ctx, msg)
	if err != nil {
		slog.Warn("[mediation] routing failed", "error", err, "message_id", util.UUIDToString(msg.ID))
		return
	}
	if decision == nil || decision.Agent == nil {
		if decision != nil && decision.SkipReason != "" {
			slog.Debug("[mediation] route skipped", "reason", decision.SkipReason)
		}
		return
	}

	slog.Info("[mediation] routed message",
		"message_id", util.UUIDToString(msg.ID),
		"channel_id", util.UUIDToString(msg.ChannelID),
		"agent", decision.Agent.Name,
		"reason", decision.Reason,
	)

	// Anti-loop check before dispatching.
	if err := s.checkAntiLoop(ctx, msg, decision.Agent.ID); err != nil {
		slog.Info("[mediation] anti-loop triggered, skipping reply",
			"reason", err.Error(),
			"message_id", util.UUIDToString(msg.ID),
			"agent", decision.Agent.Name,
		)
		return
	}

	// Dispatch through AutoReplyService — the *only* entrypoint after this point.
	// AutoReply consults the agent's auto_reply_config / api key as an INPUT.
	if s.AutoReply != nil && len(decision.Mentioned) > 0 {
		s.AutoReply.CheckAndReply(ctx, decision.Mentioned, e.WorkspaceID,
			util.UUIDToString(msg.ChannelID), msg)
	} else if s.AutoReply != nil {
		// Routed via plan/issue/capability, but no @-name to drive AutoReply by name.
		// Pass the routed agent's name so AutoReply uses the same code path.
		s.AutoReply.CheckAndReply(ctx, []string{decision.Agent.Name}, e.WorkspaceID,
			util.UUIDToString(msg.ChannelID), msg)
	}
}

// RouteMessage runs the unified routing priority and returns the agent (if any)
// that should respond. The caller decides whether to actually dispatch.
//
// Priority:
//  1. @mention — direct assignment.
//  2. Thread bound to a Plan (TODO: Plan 5 wires plan.thread_id).
//  3. Thread bound to an Issue — issue assignee responds.
//  4. Capability match — first eligible agent in the channel.
func (s *MediationService) RouteMessage(ctx context.Context, msg db.Message) (*RoutingDecision, error) {
	// 0. Skip non-routable senders.
	if msg.SenderType == "system" {
		return &RoutingDecision{Reason: "skip", SkipReason: "system_message"}, nil
	}

	workspaceID := util.UUIDToString(msg.WorkspaceID)

	// 1. @mention — direct assignment. Workspace agents are loaded so
	// parseMentionsFromContent can match names that contain whitespace
	// (e.g. "dy9759's Assistant") — the naive whitespace-split path only
	// catches the first token and misses the rest.
	wsAgents, _ := s.Queries.ListAgents(ctx, msg.WorkspaceID)
	mentions := parseMentionsFromContent(msg.Content, wsAgents)
	if len(mentions) > 0 {
		agent, err := s.routeToMentioned(ctx, workspaceID, mentions[0])
		if err == nil && agent != nil {
			return &RoutingDecision{Reason: "mention", Agent: agent, Mentioned: mentions}, nil
		}
		// Mention couldn't resolve — fall through to other strategies but still
		// expose the mentions so AutoReply can attempt a name-based dispatch.
	}

	// 2. Thread bound to a Plan.
	if msg.ThreadID.Valid {
		if plan := s.findPlanForThread(ctx, msg.ThreadID); plan != nil {
			if agent := s.routeToPlan(ctx, plan); agent != nil {
				return &RoutingDecision{Reason: "plan", Agent: agent, Mentioned: mentions}, nil
			}
		}
	}

	// 3. Thread bound to an Issue.
	if msg.ThreadID.Valid {
		if issue := s.findIssueForThread(ctx, msg.ThreadID); issue != nil {
			if agent := s.routeToIssueAssignee(ctx, issue); agent != nil {
				return &RoutingDecision{Reason: "issue_assignee", Agent: agent, Mentioned: mentions}, nil
			}
		}
	}

	// 4. Capability match (fallback).
	if agent := s.routeByCapabilityMatch(ctx, msg); agent != nil {
		return &RoutingDecision{Reason: "capability", Agent: agent, Mentioned: mentions}, nil
	}

	return &RoutingDecision{Reason: "skip", SkipReason: "no_agent_matched", Mentioned: mentions}, nil
}

// routeToMentioned resolves an @name to an agent in the workspace.
func (s *MediationService) routeToMentioned(ctx context.Context, workspaceID, name string) (*db.Agent, error) {
	a, err := s.Queries.GetAgentByName(ctx, db.GetAgentByNameParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		Name:        name,
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// findPlanForThread loads the plan (if any) bound to this thread via plan.thread_id.
// Plan 5 added the FK; if no plan owns the thread, returns nil so routing can
// fall through to the Issue branch.
func (s *MediationService) findPlanForThread(ctx context.Context, threadID pgtype.UUID) *db.Plan {
	plan, err := s.Queries.GetPlanByThread(ctx, threadID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("[mediation] findPlanForThread query failed", "error", err,
				"thread_id", util.UUIDToString(threadID))
		}
		return nil
	}
	return &plan
}

// routeToPlan picks the agent owning a plan thread.
//
// Strategy:
//  1. If the plan has an active ProjectRun, find a task on that run that is
//     currently 'running' with an actual_agent_id set; return that agent.
//  2. Otherwise fall back to the first agent listed in plan.assigned_agents.
//
// Returns nil if no agent can be resolved.
func (s *MediationService) routeToPlan(ctx context.Context, plan *db.Plan) *db.Agent {
	if plan == nil {
		return nil
	}

	// 1. Active run → currently-running task → its agent.
	if plan.ProjectID.Valid {
		run, err := s.Queries.GetActiveProjectRun(ctx, plan.ProjectID)
		if err == nil {
			tasks, err := s.Queries.ListTasksByRun(ctx, run.ID)
			if err == nil {
				for _, t := range tasks {
					if !t.ActualAgentID.Valid || t.Status != "running" {
						continue
					}
					a, err := s.Queries.GetAgent(ctx, t.ActualAgentID)
					if err != nil || a.ArchivedAt.Valid {
						continue
					}
					return &a
				}
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("[mediation] routeToPlan: GetActiveProjectRun failed",
				"error", err, "plan_id", util.UUIDToString(plan.ID))
		}
	}

	// 2. Fallback: plan.assigned_agents JSONB — shape is
	// [{"local_id": "...", "agent_id": "..."}].
	if len(plan.AssignedAgents) > 0 {
		var assignments []struct {
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal(plan.AssignedAgents, &assignments); err == nil {
			for _, asn := range assignments {
				if asn.AgentID == "" {
					continue
				}
				a, err := s.Queries.GetAgent(ctx, util.ParseUUID(asn.AgentID))
				if err != nil || a.ArchivedAt.Valid {
					continue
				}
				return &a
			}
		}
	}

	return nil
}

// findIssueForThread loads the issue (if any) that this thread is bound to.
func (s *MediationService) findIssueForThread(ctx context.Context, threadID pgtype.UUID) *db.Issue {
	t, err := s.Queries.GetThread(ctx, threadID)
	if err != nil || !t.IssueID.Valid {
		return nil
	}
	issue, err := s.Queries.GetIssue(ctx, t.IssueID)
	if err != nil {
		return nil
	}
	return &issue
}

// routeToIssueAssignee returns the assignee agent on the issue, if it is an agent.
func (s *MediationService) routeToIssueAssignee(ctx context.Context, issue *db.Issue) *db.Agent {
	if !issue.AssigneeID.Valid || !issue.AssigneeType.Valid {
		return nil
	}
	if issue.AssigneeType.String != "agent" {
		return nil
	}
	a, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		return nil
	}
	if a.ArchivedAt.Valid {
		return nil
	}
	return &a
}

// routeByCapabilityMatch picks the first eligible agent in the channel that is
// not the message sender. Plan 4 will replace this with proper capability scoring
// against agent.identity_card.capabilities.
func (s *MediationService) routeByCapabilityMatch(ctx context.Context, msg db.Message) *db.Agent {
	members, err := s.Queries.ListChannelMembers(ctx, msg.ChannelID)
	if err != nil {
		return nil
	}
	sort.SliceStable(members, func(i, j int) bool {
		left := members[i]
		right := members[j]
		if left.JoinedAt.Valid && right.JoinedAt.Valid && !left.JoinedAt.Time.Equal(right.JoinedAt.Time) {
			return left.JoinedAt.Time.Before(right.JoinedAt.Time)
		}
		if left.MemberType != right.MemberType {
			return left.MemberType < right.MemberType
		}
		return util.UUIDToString(left.MemberID) < util.UUIDToString(right.MemberID)
	})
	senderID := util.UUIDToString(msg.SenderID)

	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		if util.UUIDToString(m.MemberID) == senderID {
			continue // never route a message back to its own agent sender.
		}
		a, err := s.Queries.GetAgent(ctx, m.MemberID)
		if err != nil {
			continue
		}
		if a.ArchivedAt.Valid {
			continue
		}
		if a.AgentType == "system_agent" {
			continue // system agents are not auto-reply candidates.
		}
		return &a
	}
	return nil
}

// checkAntiLoop enforces the three anti-loop rules before dispatching a reply:
//  1. Self-reply: an agent cannot respond to its own message.
//  2. Agent-chain: refuse if the last agentChainLimit messages were all agents.
//  3. Daily cap: refuse if this agent already issued dailyAutoReplyCap replies
//     in this thread in the last 24h.
func (s *MediationService) checkAntiLoop(ctx context.Context, trigger db.Message, replierAgentID pgtype.UUID) error {
	// Rule 1: self-reply (agent → same agent).
	if trigger.SenderType == "agent" && util.UUIDToString(trigger.SenderID) == util.UUIDToString(replierAgentID) {
		return fmt.Errorf("self-reply blocked")
	}

	// Rule 2: agent-chain limit. Tail-scan the last (agentChainLimit*2) messages
	// in this thread and count consecutive agent senders ending at the latest.
	if trigger.ThreadID.Valid {
		recent, err := s.Queries.ListRecentThreadMessages(ctx, db.ListRecentThreadMessagesParams{
			ThreadID: trigger.ThreadID,
			Limit:    int32(agentChainLimit * 2),
		})
		if err == nil && len(recent) > 0 {
			consecutive := 0
			for i := len(recent) - 1; i >= 0; i-- {
				if recent[i].SenderType == "agent" {
					consecutive++
				} else {
					break
				}
			}
			if consecutive >= agentChainLimit {
				return fmt.Errorf("agent-to-agent reply limit (%d) reached", agentChainLimit)
			}
		}
	}

	// Rule 3: 24h cap on auto-replies from this agent in this thread.
	if trigger.ThreadID.Valid {
		cutoff := time.Now().Add(-24 * time.Hour)
		count, err := s.Queries.CountAgentRepliesInThread(ctx, db.CountAgentRepliesInThreadParams{
			ThreadID:  trigger.ThreadID,
			SenderID:  replierAgentID,
			CreatedAt: pgtype.Timestamptz{Time: cutoff, Valid: true},
		})
		if err == nil && count >= dailyAutoReplyCap {
			return fmt.Errorf("daily auto-reply cap (%d) reached", dailyAutoReplyCap)
		}
	}

	return nil
}

// trackReplySlot inserts a pending slot for member messages and marks pending
// slots in the channel as replied for agent messages.
func (s *MediationService) trackReplySlot(ctx context.Context, msg db.Message) {
	if s.DB == nil {
		return
	}
	switch msg.SenderType {
	case "member":
		_, _ = s.DB.Exec(ctx, `
			INSERT INTO reply_slot (message_id, channel_id, workspace_id, slot_index, content_summary, status, expires_at)
			VALUES ($1, $2, $3, 0, $4, 'pending', NOW() + INTERVAL '`+initialReplySlotExpiry+`')
		`, msg.ID, msg.ChannelID, msg.WorkspaceID, truncateStr(msg.Content, 100))
	case "agent":
		_, _ = s.DB.Exec(ctx, `
			UPDATE reply_slot SET status = 'replied', replied_at = NOW(), reply_message_id = $1
			WHERE channel_id = $2 AND status = 'pending'
		`, msg.ID, msg.ChannelID)
	}
}

// slaTier classifies a reply slot's age into one of the four tiers.
func slaTier(slot db.ReplySlot) SLAState {
	if !slot.CreatedAt.Valid {
		return SLAFresh
	}
	age := time.Since(slot.CreatedAt.Time)
	switch {
	case age < slaFallbackAfter:
		return SLAFresh
	case age < slaWarningAfter:
		return SLAFallbackAssigned
	case age < slaCriticalAfter:
		return SLAWarning
	default:
		return SLACritical
	}
}

// processSLATiers scans pending reply slots that have crossed the first
// fallback cutoff and applies the appropriate tier action. Slots that already
// progressed (status != 'pending') are ignored.
func (s *MediationService) processSLATiers(ctx context.Context) {
	if s.DB == nil {
		return
	}

	rows, err := s.DB.Query(ctx, `
		SELECT id, message_id, workspace_id, channel_id, content_summary, created_at, status
		FROM reply_slot
		WHERE status = 'pending' AND created_at < NOW() - make_interval(secs => $1)
		LIMIT 50
	`, slaFallbackAfter.Seconds())
	if err != nil {
		return
	}
	defer rows.Close()

	type slotRow struct {
		ID, MessageID, WorkspaceID, ChannelID pgtype.UUID
		Summary                               pgtype.Text
		CreatedAt                             pgtype.Timestamptz
		Status                                string
	}
	var slots []slotRow
	for rows.Next() {
		var sr slotRow
		if err := rows.Scan(&sr.ID, &sr.MessageID, &sr.WorkspaceID, &sr.ChannelID,
			&sr.Summary, &sr.CreatedAt, &sr.Status); err != nil {
			continue
		}
		slots = append(slots, sr)
	}

	for _, sr := range slots {
		slot := db.ReplySlot{
			ID:             sr.ID,
			MessageID:      sr.MessageID,
			WorkspaceID:    sr.WorkspaceID,
			ChannelID:      sr.ChannelID,
			ContentSummary: sr.Summary,
			CreatedAt:      sr.CreatedAt,
			Status:         sr.Status,
		}
		s.handleSLATier(ctx, slot)
	}
}

// handleSLATier dispatches per-tier actions for a single slot.
func (s *MediationService) handleSLATier(ctx context.Context, slot db.ReplySlot) {
	tier := slaTier(slot)
	switch tier {
	case SLAFresh:
		return
	case SLAFallbackAssigned:
		s.escalateFallback(ctx, slot)
	case SLAWarning:
		s.escalateInbox(ctx, slot, "warning")
	case SLACritical:
		s.escalateInbox(ctx, slot, "critical")
	}
}

// escalateFallback assigns a fallback agent (or notifies via system_notification
// if none can be found) and marks the slot's metadata so the next tier doesn't
// repeat the work.
func (s *MediationService) escalateFallback(ctx context.Context, slot db.ReplySlot) {
	// Mark progression so we don't reapply the fallback action; status remains
	// 'pending' so warning/critical can still escalate when their cutoff hits.
	if !s.markSlotTier(ctx, slot.ID, "fallback") {
		return // someone else already advanced it.
	}

	channelID := util.UUIDToString(slot.ChannelID)
	workspaceID := util.UUIDToString(slot.WorkspaceID)

	// Pick a fallback agent — first eligible channel agent that hasn't already replied.
	members, err := s.Queries.ListChannelMembers(ctx, slot.ChannelID)
	if err != nil {
		return
	}
	var fallback *db.Agent
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		a, err := s.Queries.GetAgent(ctx, m.MemberID)
		if err != nil || a.ArchivedAt.Valid || a.AgentType == "system_agent" {
			continue
		}
		fallback = &a
		break
	}
	if fallback == nil {
		// No fallback available — drop a notice so owner can act manually.
		s.postSystemNotice(ctx, slot.WorkspaceID, slot.ChannelID,
			"Reply still pending after 5 minutes — no fallback agent available.")
		return
	}

	// Assign the slot to the fallback agent.
	_, _ = s.DB.Exec(ctx, `UPDATE reply_slot SET assigned_agent_id = $1 WHERE id = $2`, fallback.ID, slot.ID)

	notice := fmt.Sprintf("[Mediation] @%s — please respond, primary did not reply within 5 minutes.", fallback.Name)
	s.postSystemNotice(ctx, slot.WorkspaceID, slot.ChannelID, notice)

	slog.Info("[mediation] SLA fallback assigned",
		"slot_id", util.UUIDToString(slot.ID),
		"channel_id", channelID,
		"workspace_id", workspaceID,
		"agent", fallback.Name,
	)
}

// escalateInbox creates an inbox_item to the channel's founder/owner with
// the requested severity. Marks the slot's metadata so we only post one inbox
// item per tier.
func (s *MediationService) escalateInbox(ctx context.Context, slot db.ReplySlot, severityLabel string) {
	tierKey := severityLabel // matches our tier name.
	if !s.markSlotTier(ctx, slot.ID, tierKey) {
		return // already handled at this tier.
	}

	// Resolve the recipient: the channel's founder, or the workspace founder.
	recipientID, recipientType := s.resolveOwnerForInbox(ctx, slot.WorkspaceID, slot.ChannelID)
	if !recipientID.Valid {
		slog.Debug("[mediation] no owner found for inbox escalation",
			"slot_id", util.UUIDToString(slot.ID))
		return
	}

	// inbox_item severity column constraint allows action_required/attention/info.
	// We translate plan-spec severities (warning/critical) into the existing scale:
	//   warning  -> attention
	//   critical -> action_required
	// The original plan-spec label is preserved in details.metadata.sla_tier.
	storedSeverity := "attention"
	if severityLabel == "critical" {
		storedSeverity = "action_required"
	}

	summary := "(no content)"
	if slot.ContentSummary.Valid && slot.ContentSummary.String != "" {
		summary = slot.ContentSummary.String
	}
	title := fmt.Sprintf("Reply slow (%s)", severityLabel)
	body := fmt.Sprintf("A message in this channel has not been replied to. SLA tier: %s.\n\nContent: %s",
		severityLabel, summary)

	details, _ := json.Marshal(map[string]any{
		"sla_tier":   severityLabel,
		"slot_id":    util.UUIDToString(slot.ID),
		"message_id": util.UUIDToString(slot.MessageID),
		"channel_id": util.UUIDToString(slot.ChannelID),
	})

	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   slot.WorkspaceID,
		RecipientType: recipientType,
		RecipientID:   recipientID,
		Type:          "reply_slow",
		Severity:      storedSeverity,
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: true},
		Details:       details,
	})
	if err != nil {
		slog.Warn("[mediation] inbox escalation failed",
			"error", err,
			"slot_id", util.UUIDToString(slot.ID),
			"tier", severityLabel,
		)
		return
	}

	// Populate inbox_item.slot_id with the originating reply_slot so consumers
	// can correlate the notification back to the stalled slot without parsing
	// the details JSON. CreateInboxItem does not accept slot_id in its params.
	if s.DB != nil {
		if _, err := s.DB.Exec(ctx,
			`UPDATE inbox_item SET slot_id = $1 WHERE id = $2`,
			slot.ID, item.ID,
		); err != nil {
			slog.Warn("[mediation] failed to set inbox_item.slot_id",
				"error", err,
				"inbox_item_id", util.UUIDToString(item.ID),
				"slot_id", util.UUIDToString(slot.ID),
			)
		}
	}

	slog.Info("[mediation] SLA inbox escalation",
		"slot_id", util.UUIDToString(slot.ID),
		"tier", severityLabel,
		"severity", storedSeverity,
		"recipient", util.UUIDToString(recipientID),
	)
}

// markSlotTier records the latest tier processed in the slot's content_summary
// suffix. We piggy-back on existing columns to avoid a schema migration in
// this task; Plan 4 will add a proper metadata JSONB column on reply_slot.
//
// Returns true if the tier was newly applied; false if the slot was already
// advanced to that tier (or beyond), or if the slot does not exist.
//
// The update is performed atomically in a single statement with a NOT LIKE
// guard so two workers racing on the same slot cannot both succeed and post
// duplicate inbox items.
func (s *MediationService) markSlotTier(ctx context.Context, slotID pgtype.UUID, tier string) bool {
	if s.DB == nil {
		return false
	}
	tag := strings.ToLower("[sla:" + tier + "]")

	var updated int
	err := s.DB.QueryRow(ctx, `
		UPDATE reply_slot
		SET content_summary = CASE
			WHEN content_summary IS NULL OR content_summary = '' THEN $2
			ELSE content_summary || ' ' || $2
		END
		WHERE id = $1
		  AND (content_summary IS NULL
		       OR lower(content_summary) NOT LIKE '%' || $3 || '%')
		RETURNING 1
	`, slotID, tag, tag).Scan(&updated)
	if err != nil {
		// ErrNoRows here means the guard matched (tag already present) or the
		// slot was deleted — both are "not newly applied", not a hard error.
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		return false
	}
	return updated == 1
}

// resolveOwnerForInbox finds the inbox recipient — preferring the channel
// founder, then the channel creator. Returns invalid UUID when nothing usable
// is found (caller treats that as "skip inbox").
func (s *MediationService) resolveOwnerForInbox(ctx context.Context, _ pgtype.UUID, channelID pgtype.UUID) (pgtype.UUID, string) {
	if s.DB == nil {
		return pgtype.UUID{}, ""
	}
	// Channel founder (Plan 3 added founder_id).
	row := s.DB.QueryRow(ctx, `SELECT founder_id FROM channel WHERE id = $1`, channelID)
	var founder pgtype.UUID
	if err := row.Scan(&founder); err == nil && founder.Valid {
		return founder, "member"
	}
	// Channel creator fallback.
	row = s.DB.QueryRow(ctx, `SELECT created_by, created_by_type FROM channel WHERE id = $1`, channelID)
	var createdBy pgtype.UUID
	var createdByType pgtype.Text
	if err := row.Scan(&createdBy, &createdByType); err == nil && createdBy.Valid && createdByType.Valid {
		return createdBy, createdByType.String
	}
	return pgtype.UUID{}, ""
}

// postSystemNotice writes a system_notification message in the channel as the
// workspace's system agent. Used when the bot cannot reach a real owner via
// inbox or simply needs to nudge the channel.
func (s *MediationService) postSystemNotice(ctx context.Context, workspaceID, channelID pgtype.UUID, text string) {
	sysAgent, err := s.Queries.GetSystemAgent(ctx, workspaceID)
	if err != nil {
		return
	}
	_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: workspaceID,
		SenderID:    sysAgent.ID,
		SenderType:  "agent",
		ChannelID:   channelID,
		Content:     text,
		ContentType: "text",
		Type:        "system_notification",
	})
}

// truncateStr truncates s to at most n bytes, appending "..." if truncated.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// parseMentionsFromContent extracts @agent mentions from text.
//
// The naive "split on whitespace and look for @tokens" approach breaks
// the moment an agent name contains a space (e.g. "dy9759's Assistant"),
// which the web mention-picker gladly inserts as `@dy9759's Assistant`.
// We instead iterate the known agents in this workspace and look for
// "@<name>" as a substring, preserving the whole multi-word name.
// Longer names win so "@Alpha Beta" doesn't accidentally match the
// agent named "Alpha". Falls back to the token-split scan for mentions
// whose target doesn't exist as an agent (keeps the prior behavior so
// unknown mentions surface in audit logs).
func parseMentionsFromContent(text string, agents []db.Agent) []string {
	// Longer first so name prefixes don't short-circuit longer matches.
	sorted := append([]db.Agent{}, agents...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Name) > len(sorted[j].Name)
	})

	var out []string
	seen := make(map[string]struct{})
	remaining := text
	for _, a := range sorted {
		if a.Name == "" {
			continue
		}
		needle := "@" + a.Name
		if strings.Contains(remaining, needle) {
			if _, ok := seen[a.Name]; !ok {
				seen[a.Name] = struct{}{}
				out = append(out, a.Name)
			}
			// Blank out matched slices so shorter names can't double-match.
			remaining = strings.ReplaceAll(remaining, needle, "")
		}
	}

	// Preserve the legacy token scan for @names that aren't known agents
	// (e.g. a typo, or a member mention we don't route through agents).
	for _, word := range strings.Fields(remaining) {
		if !strings.HasPrefix(word, "@") || len(word) <= 1 {
			continue
		}
		name := strings.TrimPrefix(word, "@")
		name = strings.TrimRight(name, ".,!?;:")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// messageFromMap reconstructs a db.Message from the event payload's
// message map. We accept partial fields and zero-value the rest.
//
// Payload values arrive from messageToResponse which encodes UUIDs as
// *string for JSON null-semantics; accept both bare strings and *string
// so the reconstructed message keeps a valid ChannelID / ThreadID.
func messageFromMap(m map[string]any, workspaceID string) db.Message {
	asString := func(k string) string {
		switch v := m[k].(type) {
		case string:
			return v
		case *string:
			if v == nil {
				return ""
			}
			return *v
		}
		return ""
	}
	out := db.Message{
		ID:          util.ParseUUID(asString("id")),
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    util.ParseUUID(asString("sender_id")),
		SenderType:  asString("sender_type"),
		ChannelID:   util.ParseUUID(asString("channel_id")),
		Content:     asString("content"),
		ContentType: asString("content_type"),
		Type:        asString("type"),
	}
	if tID := asString("thread_id"); tID != "" {
		out.ThreadID = util.ParseUUID(tID)
	}
	return out
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// defaultSLATimeout is the default SLA timeout for message responses.
	defaultSLATimeout = 300 * time.Second // 5 minutes
)

// MediationService subscribes to message:created events and runs mediation logic
// to determine if messages need responses and assign the best agent.
//
// It has two triggers:
//  1. Immediate @mention trigger: if a user message contains @agent, fire off
//     an auto-reply via AutoReplyService.
//  2. SLA check: after the SLA timeout, verify that a response was received.
//     If not, assign a responder and broadcast a system message.
//  3. reply_slot tracking: insert a pending slot on member messages, mark as
//     replied on agent messages, and escalate expired slots every 10 seconds.
type MediationService struct {
	Queries   *db.Queries
	Hub       *realtime.Hub
	EventBus  *events.Bus
	AutoReply *AutoReplyService
	DB        *pgxpool.Pool
}

// NewMediationService creates a new MediationService.
func NewMediationService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, autoReply *AutoReplyService, pool *pgxpool.Pool) *MediationService {
	return &MediationService{
		Queries:   q,
		Hub:       hub,
		EventBus:  bus,
		AutoReply: autoReply,
		DB:        pool,
	}
}

// Start subscribes to message:created events and runs mediation logic.
func (s *MediationService) Start() {
	ctx := context.Background()
	s.EventBus.Subscribe(protocol.EventMessageCreated, func(e events.Event) {
		go s.handleMessageCreated(e)
	})
	// Expiry checker: every 10 seconds escalate pending slots past their deadline.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkExpiredSlots(ctx)
			}
		}
	}()
	slog.Info("[mediation] service started, listening for message:created events")
}

// responseCheck holds the result of checking if a message needs a response.
type responseCheck struct {
	needsResponse bool
	reason        string
	mentionedName string // agent name if @mentioned
	mentions      []string
}

// handleMessageCreated processes a new message to determine if mediation is needed.
func (s *MediationService) handleMessageCreated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	msgData, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}

	senderType, _ := msgData["sender_type"].(string)
	content, _ := msgData["content"].(string)
	channelID, _ := msgData["channel_id"].(string)
	messageID, _ := msgData["id"].(string)
	senderID, _ := msgData["sender_id"].(string)
	workspaceID := e.WorkspaceID

	if channelID == "" {
		return // Not a channel message, skip mediation.
	}

	// Track reply slots based on who sent the message.
	if s.DB != nil {
		ctx := context.Background()
		if senderType == "member" {
			// Insert a pending reply slot for this member message.
			_, _ = s.DB.Exec(ctx, `
				INSERT INTO reply_slot (message_id, channel_id, workspace_id, slot_index, content_summary, status, expires_at)
				VALUES ($1, $2, $3, 0, $4, 'pending', NOW() + INTERVAL '30 seconds')
			`, util.ParseUUID(messageID), util.ParseUUID(channelID), util.ParseUUID(workspaceID), truncateStr(content, 100))
		} else if senderType == "agent" {
			// Mark any pending slots in this channel as replied.
			_, _ = s.DB.Exec(ctx, `
				UPDATE reply_slot SET status = 'replied', replied_at = NOW(), reply_message_id = $1
				WHERE channel_id = $2 AND status = 'pending'
			`, util.ParseUUID(messageID), util.ParseUUID(channelID))
		}
	}

	// Don't run further mediation logic for agent or system messages (prevent loops).
	if senderType == "agent" || senderType == "system" {
		return
	}

	if senderType != "member" {
		return
	}

	// Check if the message needs a response.
	check := s.checkNeedsResponse(content, channelID, workspaceID)
	if !check.needsResponse {
		return
	}

	slog.Info("[mediation] message needs response",
		"message_id", messageID,
		"channel_id", channelID,
		"reason", check.reason,
	)

	// Immediate auto-reply trigger for @mentions.
	if s.AutoReply != nil && len(check.mentions) > 0 {
		triggerMsg := db.Message{
			ID:          util.ParseUUID(messageID),
			WorkspaceID: util.ParseUUID(workspaceID),
			SenderID:    util.ParseUUID(senderID),
			SenderType:  senderType,
			ChannelID:   util.ParseUUID(channelID),
			Content:     content,
			ContentType: "text",
		}
		s.AutoReply.CheckAndReply(context.Background(), check.mentions, workspaceID, channelID, triggerMsg)
	}

	// Schedule a delayed check: after SLA timeout, verify if the message was replied to.
	// If not, assign a responder and send a system message.
	go func() {
		time.Sleep(defaultSLATimeout)
		s.checkAndAssignResponder(context.Background(), messageID, channelID, workspaceID, check)
	}()
}

// checkNeedsResponse determines if a message needs a response based on rules.
func (s *MediationService) checkNeedsResponse(content, channelID, workspaceID string) responseCheck {
	// Rule 1: Has @mention
	mentions := parseMentionsFromContent(content)
	if len(mentions) > 0 {
		return responseCheck{
			needsResponse: true,
			reason:        "has_mention",
			mentionedName: mentions[0],
			mentions:      mentions,
		}
	}

	// Rule 2: Is a question (ends with ?)
	trimmed := strings.TrimSpace(content)
	if strings.HasSuffix(trimmed, "?") {
		return responseCheck{
			needsResponse: true,
			reason:        "is_question",
		}
	}

	// Rule 3: Is in a project channel
	// TODO: wire after migration — check if channel has project_id set.
	// ch, err := s.Queries.GetChannel(context.Background(), util.ParseUUID(channelID))
	// if err == nil && ch.ProjectID.Valid {
	//     return responseCheck{needsResponse: true, reason: "project_related"}
	// }

	// Rule 4: Matches agent capability keywords
	// TODO: implement capability matching against identity_card.capabilities
	// This requires the identity_card column on agents.

	return responseCheck{needsResponse: false}
}

// checkAndAssignResponder verifies if the message was replied to within the SLA window.
// If not, it assigns an agent and sends a system notification.
func (s *MediationService) checkAndAssignResponder(ctx context.Context, messageID, channelID, workspaceID string, check responseCheck) {
	// Check if any reply was sent to this message after it was created.
	// TODO: wire after sqlc generation — expects a query like CountRepliesAfterMessage.
	// For now, check if there are newer messages in the channel.
	messages, err := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     5,
		Offset:    0,
	})
	if err != nil {
		slog.Warn("[mediation] failed to check replies", "error", err, "message_id", messageID)
		return
	}

	// If there are messages after the original, assume it was responded to.
	for _, m := range messages {
		mID := util.UUIDToString(m.ID)
		if mID != messageID && m.SenderType == "agent" {
			slog.Debug("[mediation] message already responded to", "message_id", messageID)
			return
		}
	}

	// No response found — assign a responder.
	agent := s.assignResponder(ctx, channelID, workspaceID, check)
	if agent == nil {
		slog.Warn("[mediation] no suitable agent found for response",
			"message_id", messageID,
			"channel_id", channelID,
		)
		return
	}

	// Send system message tagging the agent.
	agentName := agent.Name
	systemContent := "[System] @" + agentName + " — please respond to the above message."

	_, err = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    agent.ID, // System message attributed to system agent
		SenderType:  "system",
		ChannelID:   util.ParseUUID(channelID),
		Content:     systemContent,
		ContentType: "text",
		Type:        "system",
	})
	if err != nil {
		slog.Warn("[mediation] failed to send system message", "error", err)
		return
	}

	slog.Info("[mediation] responder assigned",
		"agent_id", util.UUIDToString(agent.ID),
		"agent_name", agentName,
		"message_id", messageID,
		"channel_id", channelID,
		"reason", check.reason,
	)

	// Broadcast event.
	data, _ := json.Marshal(map[string]any{
		"type": "mediation:responder_assigned",
		"payload": map[string]any{
			"message_id": messageID,
			"channel_id": channelID,
			"agent_id":   util.UUIDToString(agent.ID),
			"agent_name": agentName,
			"reason":     check.reason,
		},
	})
	s.Hub.BroadcastToWorkspace(workspaceID, data)
}

// assignResponder picks the best agent to respond using priority rules:
// 1. @mentioned agent
// 2. Project-assigned agent (TODO: wire after project tables exist)
// 3. Capability-matched agent
func (s *MediationService) assignResponder(ctx context.Context, channelID, workspaceID string, check responseCheck) *db.Agent {
	// Priority 1: @mentioned agent
	if check.mentionedName != "" {
		agent, err := s.Queries.GetAgentByName(ctx, db.GetAgentByNameParams{
			WorkspaceID: util.ParseUUID(workspaceID),
			Name:        check.mentionedName,
		})
		if err == nil {
			return &agent
		}
		slog.Debug("[mediation] mentioned agent not found", "name", check.mentionedName)
	}

	// Priority 2: Project-assigned agent
	// TODO: wire after project tables — check channel.project_id, find assigned agents.

	// Priority 3: Capability-matched agent from channel members.
	// For now, find any online agent that is a member of this channel.
	members, err := s.Queries.ListChannelMembers(ctx, util.ParseUUID(channelID))
	if err != nil {
		slog.Debug("[mediation] failed to list channel members", "error", err)
		return nil
	}

	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		agent, err := s.Queries.GetAgent(ctx, m.MemberID)
		if err != nil {
			continue
		}
		// Prefer agents that are not archived and are in a usable status.
		if agent.ArchivedAt.Valid {
			continue
		}
		if agent.Status == "online" || agent.Status == "idle" {
			return &agent
		}
	}

	// Fallback: find any available agent in the workspace.
	agents, err := s.Queries.ListAgents(ctx, util.ParseUUID(workspaceID))
	if err != nil || len(agents) == 0 {
		return nil
	}

	for _, a := range agents {
		if a.ArchivedAt.Valid || a.AgentType == "system_agent" {
			continue
		}
		if a.Status == "online" || a.Status == "idle" {
			return &a
		}
	}

	return nil
}

// checkExpiredSlots queries pending reply slots that have passed their expiry
// deadline, marks them as escalated, and posts a system_notification message in
// the channel so agents know to respond.
func (s *MediationService) checkExpiredSlots(ctx context.Context) {
	if s.DB == nil {
		return
	}

	rows, err := s.DB.Query(ctx, `
		SELECT id, message_id, workspace_id, channel_id, content_summary
		FROM reply_slot
		WHERE status = 'pending' AND expires_at < NOW()
		LIMIT 20
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var slotID, msgID, wsID, chID pgtype.UUID
		var summary pgtype.Text
		if err := rows.Scan(&slotID, &msgID, &wsID, &chID, &summary); err != nil {
			continue
		}

		// Mark escalated before doing any further work to prevent duplicate processing.
		_, _ = s.DB.Exec(ctx, `UPDATE reply_slot SET status = 'escalated' WHERE id = $1`, slotID)

		// Get system agent for this workspace.
		sysAgent, err := s.Queries.GetSystemAgent(ctx, wsID)
		if err != nil {
			slog.Debug("[mediation] no system agent found for workspace, skipping escalation",
				"workspace_id", util.UUIDToString(wsID))
			continue
		}

		content := "A message has not been replied to within 30 seconds. Please respond."
		if summary.Valid && summary.String != "" {
			content = fmt.Sprintf("Unreplied: \"%s\" — please respond.", summary.String)
		}

		_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
			WorkspaceID: wsID,
			SenderID:    sysAgent.ID,
			SenderType:  "agent",
			ChannelID:   chID,
			Content:     content,
			ContentType: "text",
			Type:        "system_notification",
		})

		slog.Info("[mediation] escalated expired reply slot",
			"slot_id", util.UUIDToString(slotID),
			"message_id", util.UUIDToString(msgID),
			"channel_id", util.UUIDToString(chID),
		)
	}
}

// truncateStr truncates s to at most n bytes, appending "..." if truncated.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// parseMentionsFromContent extracts @agent mentions from text.
func parseMentionsFromContent(text string) []string {
	var mentions []string
	words := strings.Fields(text)
	for _, word := range words {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			name := strings.TrimPrefix(word, "@")
			// Remove trailing punctuation.
			name = strings.TrimRight(name, ".,!?;:")
			if name != "" {
				mentions = append(mentions, name)
			}
		}
	}
	return mentions
}

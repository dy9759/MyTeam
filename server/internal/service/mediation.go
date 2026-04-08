package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// defaultSLATimeout is the default SLA timeout for message responses.
	defaultSLATimeout = 300 * time.Second // 5 minutes
)

// MediationService subscribes to message:created events and runs mediation logic
// to determine if messages need responses and assign the best agent.
type MediationService struct {
	Queries  *db.Queries
	Hub      *realtime.Hub
	EventBus *events.Bus
}

// NewMediationService creates a new MediationService.
func NewMediationService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *MediationService {
	return &MediationService{
		Queries:  q,
		Hub:      hub,
		EventBus: bus,
	}
}

// Start subscribes to message:created events and runs mediation logic.
func (s *MediationService) Start() {
	s.EventBus.Subscribe("message:created", func(e events.Event) {
		go s.handleMessageCreated(e)
	})
	slog.Info("[mediation] service started, listening for message:created events")
}

// responseCheck holds the result of checking if a message needs a response.
type responseCheck struct {
	needsResponse bool
	reason        string
	mentionedName string // agent name if @mentioned
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

	// Don't mediate messages from agents (prevent loops).
	senderType, _ := msgData["sender_type"].(string)
	if senderType == "agent" || senderType == "system" {
		return
	}

	content, _ := msgData["content"].(string)
	channelID, _ := msgData["channel_id"].(*string)
	messageID, _ := msgData["id"].(string)
	workspaceID := e.WorkspaceID

	if channelID == nil || *channelID == "" {
		return // Not a channel message, skip mediation.
	}

	// Check if the message needs a response.
	check := s.checkNeedsResponse(content, *channelID, workspaceID)
	if !check.needsResponse {
		return
	}

	slog.Info("[mediation] message needs response",
		"message_id", messageID,
		"channel_id", *channelID,
		"reason", check.reason,
	)

	// Schedule a delayed check: after SLA timeout, verify if the message was replied to.
	// If not, assign a responder and send a system message.
	go func() {
		time.Sleep(defaultSLATimeout)
		s.checkAndAssignResponder(context.Background(), messageID, *channelID, workspaceID, check)
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
		if a.ArchivedAt.Valid || a.IsSystem {
			continue
		}
		if a.Status == "online" || a.Status == "idle" {
			return &a
		}
	}

	return nil
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

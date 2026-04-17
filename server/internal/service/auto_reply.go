package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent_runner"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AutoReplyService handles @mention-triggered auto-replies from agents.
type AutoReplyService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Runner  agent_runner.AgentRunner
}

// NewAutoReplyService creates a new AutoReplyService.
func NewAutoReplyService(q *db.Queries, hub *realtime.Hub, runner agent_runner.AgentRunner) *AutoReplyService {
	return &AutoReplyService{Queries: q, Hub: hub, Runner: runner}
}

// CheckAndReply checks if any mentioned agents have auto-reply enabled and
// fires off goroutines to generate replies.
func (s *AutoReplyService) CheckAndReply(ctx context.Context, mentions []string, workspaceID string, channelID string, triggerMessage db.Message) {
	for _, mention := range mentions {
		go func(name string) {
			if err := s.replyAsMentionedAgent(ctx, name, workspaceID, channelID, triggerMessage); err != nil {
				slog.Warn("auto-reply failed", "agent", name, "error", err)
			}
		}(mention)
	}
}

var apiKeyRedactRE = regexp.MustCompile(`sk-[A-Za-z0-9\-]{6,}`)

func redactKey(s string) string {
	return apiKeyRedactRE.ReplaceAllString(s, "sk-***")
}

// loadAgentCloudLLMConfig fetches the cloud LLM config for an agent by reading
// the agent's runtime metadata under the "cloud_llm_config" key. Returns an
// empty CloudLLMConfig (and no error) when the agent has no runtime or the key
// is absent — callers must check APIKey before using.
func loadAgentCloudLLMConfig(ctx context.Context, q *db.Queries, agent db.Agent) (CloudLLMConfig, error) {
	if !agent.RuntimeID.Valid {
		return CloudLLMConfig{}, nil
	}
	runtime, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return CloudLLMConfig{}, err
	}
	return cloudLLMConfigFromRuntime(runtime), nil
}

// cloudLLMConfigFromRuntime parses runtime.metadata and extracts the
// "cloud_llm_config" entry, returning an empty struct if missing.
func cloudLLMConfigFromRuntime(runtime db.AgentRuntime) CloudLLMConfig {
	if len(runtime.Metadata) == 0 {
		return CloudLLMConfig{}
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(runtime.Metadata, &meta); err != nil {
		return CloudLLMConfig{}
	}
	raw, ok := meta["cloud_llm_config"]
	if !ok || len(raw) == 0 {
		return CloudLLMConfig{}
	}
	var cfg CloudLLMConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return CloudLLMConfig{}
	}
	return cfg
}

func (s *AutoReplyService) replyAsMentionedAgent(ctx context.Context, agentName string, workspaceID string, channelID string, trigger db.Message) error {
	agent, err := s.Queries.GetAgentByName(ctx, db.GetAgentByNameParams{
		WorkspaceID: trigger.WorkspaceID,
		Name:        agentName,
	})
	if err != nil {
		slog.Debug("auto-reply: agent not found", "name", agentName, "error", err)
		return nil
	}

	// If agent is offline, notify its owner and stop.
	if agent.Status == "offline" {
		ownerID := util.UUIDToString(agent.OwnerID)
		if ownerID != "" {
			notifContent := fmt.Sprintf("Your agent %s was mentioned but is offline. Please bring it online or respond manually.", agent.Name)
			_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
				WorkspaceID:   util.ParseUUID(workspaceID),
				SenderID:      agent.ID,
				SenderType:    "agent",
				RecipientID:   agent.OwnerID,
				RecipientType: util.StrToText("member"),
				Content:       notifContent,
				ContentType:   "text",
				Type:          "system_notification",
			})
			slog.Info("auto-reply: agent offline, notified owner", "agent", agent.Name)
		}
		return nil
	}

	// Trigger-based eligibility check removed in Account Phase 2 — auto-reply is
	// always considered for agents with auto_reply_enabled=TRUE. Plan 4 will move
	// fine-grained gating to MediationService.

	// Rate limit: skip if agent already sent >=3 consecutive messages in this channel.
	recent, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     5,
		Offset:    0,
	})
	consecutive := 0
	for i := len(recent) - 1; i >= 0; i-- {
		if util.UUIDToString(recent[i].SenderID) == util.UUIDToString(agent.ID) {
			consecutive++
		} else {
			break
		}
	}
	if consecutive >= 3 {
		slog.Info("auto-reply: rate limited", "agent", agentName, "consecutive", consecutive)
		return nil
	}

	// Load per-runtime cloud LLM config (post Account Phase 2: stored on the
	// agent's runtime metadata, not the agent row).
	cfg, err := loadAgentCloudLLMConfig(ctx, s.Queries, agent)
	if err != nil {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
			"Agent configuration is invalid: "+redactKey(err.Error()))
		return nil
	}
	if cfg.APIKey == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
			"Agent is not configured: missing API key.")
		return nil
	}

	// Build prompt with recent context.
	history, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     20,
		Offset:    0,
	})
	var sb strings.Builder
	for _, m := range history {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", util.UUIDToString(m.SenderID), m.Content))
	}
	prompt := fmt.Sprintf("Conversation history:\n%sLatest message from %s: %s\n\nReply as %s:",
		sb.String(),
		util.UUIDToString(trigger.SenderID), trigger.Content,
		agentName,
	)

	runnerCfg := agent_runner.Config{
		Kernel:       cfg.Kernel,
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
	}
	if runnerCfg.SystemPrompt == "" {
		runnerCfg.SystemPrompt = fmt.Sprintf("You are %s, an AI assistant on MyTeam. Reply concisely and helpfully.", agentName)
	}

	slog.Info("auto-reply dispatching",
		"agent", agentName,
		"channel", channelID,
		"kernel", cfg.Kernel,
		"model", cfg.Model,
	)

	reply, err := s.Runner.Run(ctx, prompt, runnerCfg)
	if err != nil {
		msg := fmt.Sprintf("Agent reply failed: %s", redactKey(err.Error()))
		slog.Warn("auto-reply runner failed", "agent", agentName, "error", err)
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID), msg)
		return nil
	}
	if reply == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
			"Agent returned empty reply.")
		return nil
	}

	// Insert agent's reply message.
	replyMsg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   util.ParseUUID(channelID),
		Content:     reply,
		ContentType: "text",
		Type:        "agent_reply",
	})
	if err != nil {
		slog.Warn("auto-reply: failed to insert reply message", "error", err)
		return err
	}

	if s.Hub != nil {
		data, _ := json.Marshal(map[string]any{"type": "message:created", "payload": messageToMap(replyMsg)})
		s.Hub.BroadcastToWorkspace(workspaceID, data)
	}

	slog.Info("auto-reply sent", "agent", agentName, "channel", channelID)
	return nil
}

// postSystemNotification sends a visible message from the agent to explain failure.
func (s *AutoReplyService) postSystemNotification(ctx context.Context, agent db.Agent, channelID, workspaceID pgtype.UUID, message string) {
	meta, _ := json.Marshal(map[string]any{"system_notification": true})
	msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: workspaceID,
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   channelID,
		Content:     message,
		ContentType: "text",
		Type:        "system_notification",
		Metadata:    meta,
	})
	if err != nil {
		slog.Warn("post system_notification failed", "error", err)
		return
	}
	if s.Hub != nil {
		data, _ := json.Marshal(map[string]any{"type": "message:created", "payload": messageToMap(msg)})
		s.Hub.BroadcastToWorkspace(util.UUIDToString(workspaceID), data)
	}
}

func messageToMap(m db.Message) map[string]any {
	return map[string]any{
		"id":           util.UUIDToString(m.ID),
		"workspace_id": util.UUIDToString(m.WorkspaceID),
		"channel_id":   util.UUIDToString(m.ChannelID),
		"sender_id":    util.UUIDToString(m.SenderID),
		"sender_type":  m.SenderType,
		"content":      m.Content,
		"content_type": m.ContentType,
		"metadata":     json.RawMessage(m.Metadata),
		"created_at":   m.CreatedAt.Time,
	}
}

// ReplyToDM handles auto-reply for direct messages sent to an agent.
// Unlike @mention replies (which go to a channel), DM replies are sent back
// to the original sender as a DM from the agent.
func (s *AutoReplyService) ReplyToDM(ctx context.Context, agentID string, workspaceID string, senderID string, trigger db.Message) {
	agent, err := s.Queries.GetAgent(ctx, util.ParseUUID(agentID))
	if err != nil {
		slog.Debug("dm-reply: agent not found", "id", agentID, "error", err)
		return
	}

	// If agent is offline, notify its owner and stop.
	if agent.Status == "offline" {
		ownerID := util.UUIDToString(agent.OwnerID)
		if ownerID != "" {
			notifContent := fmt.Sprintf("Your agent %s was mentioned but is offline. Please bring it online or respond manually.", agent.Name)
			_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
				WorkspaceID:   util.ParseUUID(workspaceID),
				SenderID:      agent.ID,
				SenderType:    "agent",
				RecipientID:   agent.OwnerID,
				RecipientType: util.StrToText("member"),
				Content:       notifContent,
				ContentType:   "text",
				Type:          "system_notification",
			})
			slog.Info("auto-reply: agent offline, notified owner", "agent", agent.Name)
		}
		return
	}

	// Load per-runtime cloud LLM config (post Account Phase 2: stored on the
	// agent's runtime metadata, not the agent row).
	cfg, cfgErr := loadAgentCloudLLMConfig(ctx, s.Queries, agent)
	if cfgErr != nil {
		slog.Warn("dm-reply: bad config", "agent", agent.Name, "error", cfgErr)
		return
	}
	if cfg.APIKey == "" {
		slog.Info("dm-reply: agent not configured (no API key)", "agent", agent.Name)
		return
	}

	// Build prompt from the trigger message.
	prompt := fmt.Sprintf("User message: %s\n\nReply as %s:", trigger.Content, agent.Name)

	runnerCfg := agent_runner.Config{
		Kernel:       cfg.Kernel,
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
	}
	if runnerCfg.SystemPrompt == "" {
		runnerCfg.SystemPrompt = fmt.Sprintf("You are %s, an AI assistant on MyTeam. Reply concisely and helpfully.", agent.Name)
	}

	slog.Info("dm-reply dispatching", "agent", agent.Name, "sender", senderID)

	reply, err := s.Runner.Run(ctx, prompt, runnerCfg)
	if err != nil {
		slog.Warn("dm-reply runner failed", "agent", agent.Name, "error", err)
		return
	}
	if reply == "" {
		return
	}

	// Send reply as DM from agent to the original sender.
	replyMsg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID:   util.ParseUUID(workspaceID),
		SenderID:      agent.ID,
		SenderType:    "agent",
		RecipientID:   util.ParseUUID(senderID),
		RecipientType: util.StrToText("member"),
		Content:       reply,
		ContentType:   "text",
		Type:          "agent_reply",
	})
	if err != nil {
		slog.Warn("dm-reply: failed to insert reply", "error", err)
		return
	}

	if s.Hub != nil {
		data, _ := json.Marshal(map[string]any{"type": "message:created", "payload": messageToMap(replyMsg)})
		s.Hub.BroadcastToWorkspace(workspaceID, data)
	}

	slog.Info("dm-reply sent", "agent", agent.Name, "sender", senderID)
}

// StartPollDaemon starts a background loop that checks for unread messages every 5 seconds
// and triggers auto-reply for agents with auto_reply_enabled.
func (s *AutoReplyService) StartPollDaemon(ctx context.Context) {
	slog.Info("[auto-reply] Poll daemon started (5s interval)")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("[auto-reply] Poll daemon stopped")
			return
		case <-ticker.C:
			s.pollAndReply(ctx)
		}
	}
}

func (s *AutoReplyService) pollAndReply(ctx context.Context) {
	// GetAutoReplyAgents requires a workspace_id, so we pass a zero UUID
	// to get agents across all workspaces.
	agents, err := s.Queries.GetAutoReplyAgents(ctx, pgtype.UUID{})
	if err != nil {
		slog.Debug("[auto-reply] poll: failed to get auto-reply agents", "error", err)
		return
	}

	for _, agent := range agents {
		count, _ := s.Queries.CountUnreadMessages(ctx, db.CountUnreadMessagesParams{
			RecipientID:   agent.ID,
			RecipientType: util.StrToText("agent"),
		})
		if count > 0 {
			slog.Info("[auto-reply] Unread messages for agent", "agent", agent.Name, "count", count)
			// TODO: fetch messages and generate replies
		}
	}
}

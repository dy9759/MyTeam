package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
)

// AutoReplyConfig describes the LLM configuration stored in agent.auto_reply_config.
type AutoReplyConfig struct {
	Enabled      bool   `json:"enabled"`
	LLMEndpoint  string `json:"llm_endpoint,omitempty"`
	LLMApiKey    string `json:"llm_api_key,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Fallback     *struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		ApiKey   string `json:"api_key"`
		Model    string `json:"model"`
	} `json:"fallback,omitempty"`
}

// AutoReplyService handles @mention-triggered auto-replies from agents.
type AutoReplyService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
}

// NewAutoReplyService creates a new AutoReplyService.
func NewAutoReplyService(q *db.Queries, hub *realtime.Hub) *AutoReplyService {
	return &AutoReplyService{Queries: q, Hub: hub}
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

func (s *AutoReplyService) replyAsMentionedAgent(ctx context.Context, agentName string, workspaceID string, channelID string, trigger db.Message) error {
	// Look up agent by name in workspace.
	agent, err := s.Queries.GetAgentByName(ctx, db.GetAgentByNameParams{
		WorkspaceID: trigger.WorkspaceID,
		Name:        agentName,
	})
	if err != nil {
		slog.Debug("auto-reply: agent not found by name", "name", agentName, "error", err)
		return nil // not an error — mention might not refer to an agent
	}

	if !agent.AutoReplyEnabled.Bool {
		slog.Debug("auto-reply: agent has auto-reply disabled", "agent", agentName)
		return nil
	}

	// Parse auto-reply config.
	var cfg AutoReplyConfig
	if len(agent.AutoReplyConfig) > 0 {
		if err := json.Unmarshal(agent.AutoReplyConfig, &cfg); err != nil {
			slog.Warn("auto-reply: bad config JSON", "agent", agentName, "error", err)
			return err
		}
	}

	// Rate limit: skip if agent already sent >=3 consecutive messages in this channel
	recentMsgs, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     5,
		Offset:    0,
	})
	consecutiveCount := 0
	// Messages are ordered ASC, so iterate from the end (most recent first)
	for i := len(recentMsgs) - 1; i >= 0; i-- {
		m := recentMsgs[i]
		if util.UUIDToString(m.SenderID) == util.UUIDToString(agent.ID) {
			consecutiveCount++
		} else {
			break // stop counting at first non-agent message
		}
	}
	if consecutiveCount >= 3 {
		slog.Info("auto-reply: rate limited", "agent", agentName, "consecutive", consecutiveCount)
		return nil
	}

	contentPreview := trigger.Content
	if len(contentPreview) > 50 {
		contentPreview = contentPreview[:50]
	}
	slog.Info("auto-reply triggered",
		"agent", agentName,
		"channel", channelID,
		"trigger_content", contentPreview,
		"llm_endpoint", cfg.LLMEndpoint,
		"model", cfg.Model,
	)

	// 1. Get recent channel messages for context
	messages, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     20,
		Offset:    0,
	})

	var history strings.Builder
	for _, m := range messages {
		history.WriteString(fmt.Sprintf("[%s]: %s\n", util.UUIDToString(m.SenderID), m.Content))
	}

	// 2. Call LLM via unified client.
	llmCfg := llmclient.FromAgentConfig(cfg.LLMEndpoint, cfg.LLMApiKey, cfg.Model)

	systemPrompt := fmt.Sprintf("You are %s, an AI agent. Respond naturally based on conversation context. Keep responses concise.", agentName)
	if cfg.SystemPrompt != "" {
		systemPrompt = cfg.SystemPrompt
	}

	replyText := ""
	if llmCfg.APIKey != "" {
		userMsg := fmt.Sprintf("Conversation:\n%s\nLatest message from %s: %s\n\nRespond:", history.String(), util.UUIDToString(trigger.SenderID), trigger.Content)
		text, err := llmclient.New(llmCfg).Chat(ctx, systemPrompt, []llmclient.Message{
			{Role: "user", Content: userMsg},
		})
		if err != nil {
			slog.Warn("auto-reply: LLM call failed", "agent", agentName, "error", err)
		} else {
			replyText = text
		}
	}

	if replyText == "" {
		replyText = fmt.Sprintf("Hi! I'm %s. I noticed you mentioned me. How can I help?", agentName)
	}

	// 3. Send reply as agent
	_, err = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   util.ParseUUID(channelID),
		Content:     replyText,
		ContentType: "text",
	})

	slog.Info("auto-reply sent", "agent", agentName, "channel", channelID)
	return err
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
	// to get agents across all workspaces. If the query filters by workspace,
	// we iterate known workspaces. For now, use zero UUID as a best-effort scan.
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


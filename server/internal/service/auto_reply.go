package service

import (
	"context"
	"encoding/json"
	"log/slog"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/realtime"
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

	// TODO: implement LLM call
	// 1. Get channel message history for context
	// 2. Call LLM (Anthropic or fallback provider)
	// 3. Create reply message as agent
	// 4. Publish WS event

	return nil
}

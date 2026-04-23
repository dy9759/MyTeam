package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	"github.com/MyAIOSHub/MyTeam/server/pkg/agent_runner"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

// AutoReplyService handles @mention-triggered auto-replies from agents.
type AutoReplyService struct {
	Queries          *db.Queries
	Hub              *realtime.Hub
	Runner           agent_runner.AgentRunner
	ConversationRuns *ConversationAgentRunService
}

// NewAutoReplyService creates a new AutoReplyService.
func NewAutoReplyService(q *db.Queries, hub *realtime.Hub, runner agent_runner.AgentRunner, conversationRuns ...*ConversationAgentRunService) *AutoReplyService {
	svc := &AutoReplyService{Queries: q, Hub: hub, Runner: runner}
	if len(conversationRuns) > 0 {
		svc.ConversationRuns = conversationRuns[0]
	}
	return svc
}

// CheckAndReply checks if any mentioned agents have auto-reply enabled and
// fires off goroutines to generate replies. Callers (e.g. MediationService)
// hand in a request-scoped ctx that gets cancelled when the handler returns;
// we detach here so the async LLM call isn't killed before it responds.
func (s *AutoReplyService) CheckAndReply(ctx context.Context, mentions []string, workspaceID string, channelID string, triggerMessage db.Message) {
	for _, mention := range mentions {
		go func(name string) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := s.replyAsMentionedAgent(bgCtx, name, workspaceID, channelID, triggerMessage); err != nil {
				slog.Warn("auto-reply failed", "agent", name, "error", err)
			}
		}(mention)
	}
}

var apiKeyRedactRE = regexp.MustCompile(`sk-[A-Za-z0-9\-]{6,}`)

func redactKey(s string) string {
	return apiKeyRedactRE.ReplaceAllString(s, "sk-***")
}

// senderNameCache keys by "<type>:<id>" so agent and member UUIDs can
// coexist without collision. buildSenderNameCache resolves every
// distinct sender in the history + trigger so the transcript handed
// to the LLM reads as human names rather than raw UUIDs.
type senderNameCache map[string]string

func (s *AutoReplyService) buildSenderNameCache(
	ctx context.Context,
	workspaceID pgtype.UUID,
	history []db.Message,
	trigger db.Message,
) senderNameCache {
	cache := make(senderNameCache)
	seen := make(map[string]struct{})
	collect := func(id pgtype.UUID, t string) {
		key := t + ":" + util.UUIDToString(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		switch t {
		case "agent":
			if a, err := s.Queries.GetAgent(ctx, id); err == nil {
				if a.DisplayName.Valid && a.DisplayName.String != "" {
					cache[key] = a.DisplayName.String
				} else {
					cache[key] = a.Name
				}
			}
		case "member":
			// message.sender_id for members holds the user_id; grab the
			// display name from the user row (members table has no name).
			if u, err := s.Queries.GetUser(ctx, id); err == nil {
				name := u.Name
				if name == "" {
					name = u.Email
				}
				if name != "" {
					cache[key] = name
				}
			}
		}
		_ = workspaceID
	}
	for _, m := range history {
		collect(m.SenderID, m.SenderType)
	}
	collect(trigger.SenderID, trigger.SenderType)
	return cache
}

func resolveSenderName(cache senderNameCache, id pgtype.UUID, senderType string) string {
	key := senderType + ":" + util.UUIDToString(id)
	if name, ok := cache[key]; ok && name != "" {
		return name
	}
	return util.UUIDToString(id)
}

// broadcastAgentTyping emits a "typing" WS event so the UI can show an
// "agent is typing" pill while the LLM runs. Exactly one of channelID /
// recipientID should be set — channel mentions get channel_id, DM
// replies get recipient_id.
func (s *AutoReplyService) broadcastAgentTyping(
	workspaceID, senderID string,
	channelID, recipientID *string,
	isTyping bool,
) {
	if s.Hub == nil {
		return
	}
	payload := map[string]any{
		"sender_id":   senderID,
		"sender_type": "agent",
		"is_typing":   isTyping,
	}
	if channelID != nil {
		payload["channel_id"] = *channelID
	}
	if recipientID != nil {
		payload["recipient_id"] = *recipientID
	}
	data, err := json.Marshal(map[string]any{"type": "typing", "payload": payload})
	if err != nil {
		return
	}
	s.Hub.BroadcastToWorkspace(workspaceID, data)
}

// loadAgentCloudLLMConfig fetches the cloud LLM config for an agent by
// reading the agent's runtime metadata under the "cloud_llm_config" key.
// Page-scoped system agents (Account/Conversation/Project/File Agent)
// don't have per-runtime metadata — they share the workspace's cloud
// runtime, which is only guaranteed to exist, not to carry cloud_llm_config.
// In that case we fall back to the process-wide AGENT_LLM_* env so every
// system agent reaches the same Claude Agent SDK configuration.
func loadAgentCloudLLMConfig(ctx context.Context, q *db.Queries, agent db.Agent) (CloudLLMConfig, error) {
	envCfg := LoadCloudLLMConfigFromEnv()
	if !agent.RuntimeID.Valid {
		return envCfg, nil
	}
	runtime, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return envCfg, err
	}
	cfg := cloudLLMConfigFromRuntime(runtime)
	if cfg.APIKey == "" {
		return envCfg, nil
	}
	return cfg, nil
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

	// Local-runtime branch: if the mentioned agent is backed by a local
	// runtime (daemon-registered CLI), enqueue the reply instead of calling
	// the cloud LLM directly. Parity with ReplyToDM's local branch.
	if agent.RuntimeID.Valid {
		runtime, runtimeErr := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if runtimeErr == nil && runtime.Mode.Valid && runtime.Mode.String == "local" {
			if runtime.Status == "offline" {
				s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
					"Local runtime is offline. Please start MyTeam daemon and try again.")
				return nil
			}
			if s.ConversationRuns == nil {
				slog.Warn("auto-reply: local runtime selected but conversation run service is missing", "agent", agent.Name)
				s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
					"Local agent queue is not configured on the server.")
				return nil
			}
			prompt := s.buildChannelMentionPrompt(ctx, workspaceID, channelID, agent, trigger)
			if _, err := s.ConversationRuns.EnqueueChannelMentionRun(ctx, ConversationAgentRunInput{
				WorkspaceID:      workspaceID,
				TriggerMessageID: util.UUIDToString(trigger.ID),
				AgentID:          util.UUIDToString(agent.ID),
				RuntimeID:        util.UUIDToString(runtime.ID),
				ChannelID:        channelID,
				ThreadID:         util.UUIDToString(trigger.ThreadID),
				Provider:         runtime.Provider,
				Prompt:           prompt,
				Metadata: map[string]any{
					"runtime_id": util.UUIDToString(runtime.ID),
					"runtime":    runtime.Name,
				},
			}); err != nil {
				slog.Warn("auto-reply: failed to enqueue local run", "agent", agent.Name, "error", err)
				s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
					"Local agent run failed to queue: "+redactKey(err.Error()))
				return nil
			}
			slog.Info("auto-reply queued for local runtime", "agent", agent.Name, "runtime", runtime.Name, "provider", runtime.Provider)
			return nil
		}
	}

	// Load per-runtime cloud LLM config (post Account Phase 2: stored on the
	// agent's runtime metadata, not the agent row).
	cfg, err := loadAgentCloudLLMConfig(ctx, s.Queries, agent)
	if err != nil {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
			"Agent configuration is invalid: "+redactKey(err.Error()))
		return nil
	}
	if cfg.APIKey == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
			"Agent is not configured: missing API key.")
		return nil
	}

	// Build prompt with recent context. Resolve sender IDs to display
	// names so the model sees a human-readable transcript instead of
	// raw UUIDs — multi-turn follow-ups like "what did we just discuss"
	// depend on the agent recognizing the participants in the history.
	history, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     20,
		Offset:    0,
	})
	nameCache := s.buildSenderNameCache(ctx, util.ParseUUID(workspaceID), history, trigger)
	var sb strings.Builder
	for _, m := range history {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", resolveSenderName(nameCache, m.SenderID, m.SenderType), m.Content))
	}
	prompt := fmt.Sprintf("Conversation history:\n%sLatest message from %s: %s\n\nReply as %s:",
		sb.String(),
		resolveSenderName(nameCache, trigger.SenderID, trigger.SenderType), trigger.Content,
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

	// Surface "agent is typing" in the UI while the LLM is running.
	// Must be paired with a matching is_typing=false after the reply is
	// posted so the indicator clears even on Runner failure.
	s.broadcastAgentTyping(workspaceID, util.UUIDToString(agent.ID), &channelID, nil, true)
	defer s.broadcastAgentTyping(workspaceID, util.UUIDToString(agent.ID), &channelID, nil, false)

	reply, err := s.Runner.Run(ctx, prompt, runnerCfg)
	if err != nil {
		msg := fmt.Sprintf("Agent reply failed: %s", redactKey(err.Error()))
		slog.Warn("auto-reply runner failed", "agent", agentName, "error", err)
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID), msg)
		return nil
	}
	if reply == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), trigger.ThreadID, util.ParseUUID(workspaceID),
			"Agent returned empty reply.")
		return nil
	}

	// Insert agent's reply message. If the trigger lived inside a thread,
	// the reply must be attached to that thread so ListMessagesByThread and
	// MediationService anti-loop checks see it.
	replyMsg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   util.ParseUUID(channelID),
		ThreadID:    trigger.ThreadID,
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

// postSystemNotification sends a visible message from the agent to explain
// failure. If threadID is valid, the notification is attached to the same
// thread as the triggering message so it threads cleanly in the UI.
func (s *AutoReplyService) postSystemNotification(ctx context.Context, agent db.Agent, channelID, threadID, workspaceID pgtype.UUID, message string) {
	meta, _ := json.Marshal(map[string]any{"system_notification": true})
	msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: workspaceID,
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   channelID,
		ThreadID:    threadID,
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

func (s *AutoReplyService) postDMSystemNotification(ctx context.Context, agent db.Agent, recipientID, workspaceID pgtype.UUID, message string) {
	meta, _ := json.Marshal(map[string]any{"system_notification": true})
	msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID:   workspaceID,
		SenderID:      agent.ID,
		SenderType:    "agent",
		RecipientID:   recipientID,
		RecipientType: util.StrToText("member"),
		Content:       message,
		ContentType:   "text",
		Type:          "system_notification",
		Metadata:      meta,
	})
	if err != nil {
		slog.Warn("post dm system_notification failed", "error", err)
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
		"recipient_id": util.UUIDToString(m.RecipientID),
		"sender_id":    util.UUIDToString(m.SenderID),
		"sender_type":  m.SenderType,
		"content":      m.Content,
		"content_type": m.ContentType,
		"type":         m.Type,
		"status":       m.Status,
		"metadata":     json.RawMessage(m.Metadata),
		"created_at":   m.CreatedAt.Time,
		"updated_at":   m.UpdatedAt.Time,
	}
}

// buildChannelMentionPrompt composes the recent channel transcript into
// a single prompt the local runtime can consume. Mirrors buildDMPrompt
// but pulls history via ListChannelMessages.
func (s *AutoReplyService) buildChannelMentionPrompt(ctx context.Context, workspaceID string, channelID string, agent db.Agent, trigger db.Message) string {
	history, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     40,
		Offset:    0,
	})
	nameCache := s.buildSenderNameCache(ctx, util.ParseUUID(workspaceID), history, trigger)
	var sb strings.Builder
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", resolveSenderName(nameCache, m.SenderID, m.SenderType), m.Content))
	}
	return fmt.Sprintf("Channel transcript:\n%sReply as %s to the latest message (you were @mentioned):",
		sb.String(), agent.Name,
	)
}

func (s *AutoReplyService) buildDMPrompt(ctx context.Context, workspaceID string, senderID string, agent db.Agent, trigger db.Message) string {
	// Build prompt with recent DM history so the agent has multi-turn memory.
	// History is ordered chronologically; the trigger is the last row so no
	// duplicate append is needed.
	history, _ := s.Queries.ListDMMessages(ctx, db.ListDMMessagesParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SelfID:      agent.ID,
		SelfType:    "agent",
		PeerID:      util.ParseUUID(senderID),
		PeerType:    util.StrToText("member"),
		LimitCount:  40,
		OffsetCount: 0,
	})
	nameCache := s.buildSenderNameCache(ctx, util.ParseUUID(workspaceID), history, trigger)
	var sb strings.Builder
	for _, m := range history {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", resolveSenderName(nameCache, m.SenderID, m.SenderType), m.Content))
	}
	return fmt.Sprintf("Conversation history:\n%sReply as %s to the latest message:",
		sb.String(), agent.Name,
	)
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

	if agent.RuntimeID.Valid {
		runtime, runtimeErr := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if runtimeErr == nil && runtime.Mode.Valid && runtime.Mode.String == "local" {
			if runtime.Status == "offline" {
				s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
					"Local runtime is offline. Please start MyTeam daemon and try again.")
				return
			}
			if s.ConversationRuns == nil {
				slog.Warn("dm-reply: local runtime selected but conversation run service is missing", "agent", agent.Name)
				s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
					"Local agent queue is not configured on the server.")
				return
			}
			prompt := s.buildDMPrompt(ctx, workspaceID, senderID, agent, trigger)
			if _, err := s.ConversationRuns.EnqueueDMRun(ctx, ConversationAgentRunInput{
				WorkspaceID:      workspaceID,
				TriggerMessageID: util.UUIDToString(trigger.ID),
				AgentID:          util.UUIDToString(agent.ID),
				RuntimeID:        util.UUIDToString(runtime.ID),
				PeerUserID:       senderID,
				Provider:         runtime.Provider,
				Prompt:           prompt,
				Metadata: map[string]any{
					"runtime_id": util.UUIDToString(runtime.ID),
					"runtime":    runtime.Name,
				},
			}); err != nil {
				slog.Warn("dm-reply: failed to enqueue local run", "agent", agent.Name, "error", err)
				s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
					"Local agent run failed to queue: "+redactKey(err.Error()))
				return
			}
			slog.Info("dm-reply queued for local runtime", "agent", agent.Name, "runtime", runtime.Name, "provider", runtime.Provider)
			return
		}
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
		s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
			"Agent configuration is invalid: "+redactKey(cfgErr.Error()))
		return
	}
	if cfg.APIKey == "" {
		slog.Info("dm-reply: agent not configured (no API key)", "agent", agent.Name)
		s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
			"Agent is not configured: missing API key.")
		return
	}

	prompt := s.buildDMPrompt(ctx, workspaceID, senderID, agent, trigger)

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

	s.broadcastAgentTyping(workspaceID, util.UUIDToString(agent.ID), nil, &senderID, true)
	defer s.broadcastAgentTyping(workspaceID, util.UUIDToString(agent.ID), nil, &senderID, false)

	reply, err := s.Runner.Run(ctx, prompt, runnerCfg)
	if err != nil {
		slog.Warn("dm-reply runner failed", "agent", agent.Name, "error", err)
		s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
			"Agent reply failed: "+redactKey(err.Error()))
		return
	}
	if reply == "" {
		s.postDMSystemNotification(ctx, agent, util.ParseUUID(senderID), util.ParseUUID(workspaceID),
			"Agent returned empty reply.")
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

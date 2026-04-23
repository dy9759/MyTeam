package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MyAIOSHub/MyTeam/server/pkg/agent"
)

type conversationRunResult struct {
	Output    string
	SessionID string
	WorkDir   string
}

func (d *Daemon) handleConversationRun(ctx context.Context, run ConversationRun) {
	d.mu.Lock()
	rt := d.runtimeIndex[run.RuntimeID]
	d.mu.Unlock()
	provider := run.Provider
	if provider == "" && rt.Provider != "" {
		provider = rt.Provider
	}

	runLog := d.logger.With("conversation_run", shortID(run.ID), "provider", provider)
	runLog.Info("picked conversation run", "agent", run.AgentID, "peer_user", run.PeerUserID)

	if err := d.client.StartConversationRun(ctx, run.ID); err != nil {
		runLog.Error("start conversation run failed", "error", err)
		if failErr := d.client.FailConversationRun(ctx, run.ID, fmt.Sprintf("start conversation run failed: %s", err.Error())); failErr != nil {
			runLog.Error("fail conversation run after start error", "error", failErr)
		}
		return
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	cancelledByPoll := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if status, err := d.client.GetConversationRunStatus(ctx, run.ID); err == nil && status == "cancelled" {
					runLog.Info("conversation run cancelled by server, interrupting agent")
					runCancel()
					close(cancelledByPoll)
					return
				}
			}
		}
	}()

	result, err := d.runConversationRun(runCtx, run, provider, runLog)

	select {
	case <-cancelledByPoll:
		runLog.Info("conversation run cancelled during execution, discarding result")
		return
	default:
	}

	if err != nil {
		runLog.Error("conversation run failed", "error", err)
		if failErr := d.client.FailConversationRun(ctx, run.ID, err.Error()); failErr != nil {
			runLog.Error("fail conversation run callback failed", "error", failErr)
		}
		return
	}

	if err := d.client.CompleteConversationRun(ctx, run.ID, ConversationRunCompleteRequest{
		Output:    result.Output,
		SessionID: result.SessionID,
		WorkDir:   result.WorkDir,
	}); err != nil {
		runLog.Error("complete conversation run failed, falling back to fail", "error", err)
		if failErr := d.client.FailConversationRun(ctx, run.ID, fmt.Sprintf("complete conversation run failed: %s", err.Error())); failErr != nil {
			runLog.Error("fail conversation run fallback also failed", "error", failErr)
		}
	}
}

func (d *Daemon) runConversationRun(ctx context.Context, run ConversationRun, provider string, runLog *slog.Logger) (conversationRunResult, error) {
	entry, ok := d.cfg.Agents[provider]
	if !ok {
		return conversationRunResult{}, fmt.Errorf("no agent configured for provider %q", provider)
	}

	workDir, err := d.prepareConversationWorkDir(run, provider)
	if err != nil {
		return conversationRunResult{}, err
	}

	agentEnv := map[string]string{
		"MYTEAM_CONVERSATION_RUN_ID":     run.ID,
		"MYTEAM_CONVERSATION_PEER_ID":    run.PeerUserID,
		"MYTEAM_CONVERSATION_RUNTIME_ID": run.RuntimeID,
		"MYTEAM_AGENT_ID":                run.AgentID,
		"MYTEAM_DAEMON_PORT":             fmt.Sprintf("%d", d.cfg.HealthPort),
		"MYTEAM_SERVER_URL":              d.cfg.ServerBaseURL,
		"MYTEAM_TOKEN":                   d.client.Token(),
		"MYTEAM_WORKSPACE_ID":            run.WorkspaceID,
	}

	backendType, sessionKey := d.selectConversationBackendType(provider, run)
	agentCfg := agent.Config{
		ExecutablePath: entry.Path,
		Env:            agentEnv,
		Logger:         d.logger,
	}

	backend, err := agent.New(backendType, agentCfg)
	if err != nil {
		if backendType != provider {
			d.logger.Warn("conversation claude-persistent backend init failed; falling back to single-shot", "error", err)
			backend, err = agent.New(provider, agentCfg)
			backendType = provider
			sessionKey = ""
		}
		if err != nil {
			return conversationRunResult{}, fmt.Errorf("create agent backend: %w", err)
		}
	}

	runLog.Info("starting conversation agent",
		"provider", provider,
		"backend", backendType,
		"workdir", workDir,
		"model", entry.Model,
	)

	execOpts := agent.ExecOptions{
		Cwd:        workDir,
		Model:      entry.Model,
		Timeout:    d.cfg.AgentTimeout,
		SessionKey: sessionKey,
	}

	session, err := backend.Execute(ctx, run.Prompt, execOpts)
	if err != nil && backendType == "claude-persistent" {
		d.logger.Warn("conversation claude-persistent Execute failed; falling back to single-shot", "error", err)
		if fallback, fbErr := agent.New(provider, agentCfg); fbErr == nil {
			execOpts.SessionKey = ""
			session, err = fallback.Execute(ctx, run.Prompt, execOpts)
		}
	}
	if err != nil {
		return conversationRunResult{}, err
	}

	var toolCount atomic.Int32
	var outputMu sync.Mutex
	var streamedOutput strings.Builder
	drainDone := make(chan struct{})

	go func() {
		defer close(drainDone)
		var seq atomic.Int64
		var mu sync.Mutex
		var pendingText strings.Builder
		var pendingThinking strings.Builder
		var batch []ConversationRunEventData
		callIDToTool := map[string]string{}

		flush := func() {
			mu.Lock()
			if pendingThinking.Len() > 0 {
				s := seq.Add(1)
				batch = append(batch, ConversationRunEventData{
					Seq:     s,
					Type:    "thinking",
					Content: pendingThinking.String(),
				})
				pendingThinking.Reset()
			}
			if pendingText.Len() > 0 {
				text := pendingText.String()
				s := seq.Add(1)
				batch = append(batch, ConversationRunEventData{
					Seq:     s,
					Type:    "text",
					Content: text,
				})
				outputMu.Lock()
				streamedOutput.WriteString(text)
				outputMu.Unlock()
				pendingText.Reset()
			}
			toSend := batch
			batch = nil
			mu.Unlock()

			if len(toSend) > 0 {
				sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := d.client.ReportConversationRunEvents(sendCtx, run.ID, toSend); err != nil {
					runLog.Debug("failed to report conversation run events", "error", err)
				}
				cancel()
			}
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		flusherDone := make(chan struct{})
		go func() {
			for {
				select {
				case <-ticker.C:
					flush()
				case <-flusherDone:
					return
				}
			}
		}()

		for msg := range session.Messages {
			switch msg.Type {
			case agent.MessageToolUse:
				n := toolCount.Add(1)
				runLog.Info(fmt.Sprintf("tool #%d: %s", n, msg.Tool))
				if msg.CallID != "" {
					mu.Lock()
					callIDToTool[msg.CallID] = msg.Tool
					mu.Unlock()
				}
				s := seq.Add(1)
				mu.Lock()
				batch = append(batch, ConversationRunEventData{
					Seq:   s,
					Type:  "tool_use",
					Tool:  msg.Tool,
					Input: msg.Input,
				})
				mu.Unlock()
			case agent.MessageToolResult:
				s := seq.Add(1)
				output := msg.Output
				if len(output) > 8192 {
					output = output[:8192]
				}
				toolName := msg.Tool
				if toolName == "" && msg.CallID != "" {
					mu.Lock()
					toolName = callIDToTool[msg.CallID]
					mu.Unlock()
				}
				mu.Lock()
				batch = append(batch, ConversationRunEventData{
					Seq:    s,
					Type:   "tool_result",
					Tool:   toolName,
					Output: output,
				})
				mu.Unlock()
			case agent.MessageThinking:
				if msg.Content != "" {
					mu.Lock()
					pendingThinking.WriteString(msg.Content)
					mu.Unlock()
				}
			case agent.MessageText:
				if msg.Content != "" {
					runLog.Debug("agent", "text", truncateLog(msg.Content, 200))
					mu.Lock()
					pendingText.WriteString(msg.Content)
					mu.Unlock()
				}
			case agent.MessageError:
				runLog.Error("agent error", "content", msg.Content)
				s := seq.Add(1)
				mu.Lock()
				batch = append(batch, ConversationRunEventData{
					Seq:     s,
					Type:    "error",
					Content: msg.Content,
					Error:   msg.Content,
				})
				mu.Unlock()
			}
		}

		close(flusherDone)
		flush()
	}()

	result := <-session.Result
	select {
	case <-drainDone:
	case <-time.After(2 * time.Second):
		runLog.Debug("timed out waiting for conversation message drain")
	}

	runLog.Info("conversation agent finished",
		"status", result.Status,
		"tools", toolCount.Load(),
	)

	switch result.Status {
	case "completed":
		output := result.Output
		if output == "" {
			outputMu.Lock()
			output = streamedOutput.String()
			outputMu.Unlock()
		}
		if output == "" {
			return conversationRunResult{}, fmt.Errorf("%s returned empty output", provider)
		}
		return conversationRunResult{Output: output, SessionID: result.SessionID, WorkDir: workDir}, nil
	case "timeout":
		return conversationRunResult{}, fmt.Errorf("%s timed out after %s", provider, d.cfg.AgentTimeout)
	default:
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s execution %s", provider, result.Status)
		}
		return conversationRunResult{}, fmt.Errorf("%s", errMsg)
	}
}

func (d *Daemon) prepareConversationWorkDir(run ConversationRun, provider string) (string, error) {
	if strings.TrimSpace(run.WorkDir) != "" {
		return run.WorkDir, nil
	}
	dir := filepath.Join(d.cfg.WorkspacesRoot, "conversations", run.WorkspaceID, provider, run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare conversation workdir: %w", err)
	}
	return dir, nil
}

func (d *Daemon) selectConversationBackendType(provider string, run ConversationRun) (string, string) {
	if provider == "claude" && d.cfg.ClaudeMode == ClaudeModePersistent {
		return "claude-persistent", run.AgentID + ":" + run.PeerUserID
	}
	return provider, ""
}

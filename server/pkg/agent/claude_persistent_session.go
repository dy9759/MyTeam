package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// persistentSession owns a single long-lived claude child process and the
// goroutines that read its stdout, auto-approve control requests, and serve
// one turn at a time.
type persistentSession struct {
	backend *claudePersistentBackend
	key     string // empty for ephemeral single-shot

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	stdinMu sync.Mutex
	turnMu  sync.Mutex

	aliveMu sync.RWMutex
	alive   bool

	waitOnce sync.Once
	waitDone chan struct{}
	waitMu   sync.Mutex
	waitErr  error

	// turn is swapped under stateMu and read by the reader goroutine via
	// loadTurn — callers must treat its fields as valid only while the
	// turnMu is held (one turn at a time).
	stateMu sync.RWMutex
	turn    *turnState

	// idleTimer fires after idleTimeout; nil for ephemeral sessions.
	idleTimer *time.Timer

	shutdown chan struct{}
}

// turnState captures the per-turn mutable state the reader goroutine writes
// into. A pointer is swapped atomically under persistentSession.stateMu.
type turnState struct {
	msgCh     chan Message
	resCh     chan Result
	output    strings.Builder
	sessionID string
	startTime time.Time
	timeout   time.Duration

	finalStatus string
	finalError  string

	done chan struct{}
}

// ── Session liveness ──

func (s *persistentSession) isAlive() bool {
	s.aliveMu.RLock()
	defer s.aliveMu.RUnlock()
	return s.alive
}

func (s *persistentSession) markDead() {
	s.aliveMu.Lock()
	wasAlive := s.alive
	s.alive = false
	s.aliveMu.Unlock()
	if wasAlive {
		close(s.shutdown)
	}
}

// ── Idle timer ──

func (s *persistentSession) cancelIdleTimer() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

func (s *persistentSession) scheduleIdleCleanup() {
	if s.key == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.backend.idleTimeout, func() {
		s.backend.cfg.Logger.Info("claude-persistent: idle timeout — closing session", "key", s.key)
		s.backend.evict(s.key, s)
		s.shutdownProcess()
	})
}

func (s *persistentSession) waitProcess() <-chan struct{} {
	s.waitOnce.Do(func() {
		go func() {
			err := s.cmd.Wait()
			s.waitMu.Lock()
			s.waitErr = err
			s.waitMu.Unlock()
			close(s.waitDone)
		}()
	})
	return s.waitDone
}

func (s *persistentSession) processWaitErr() error {
	<-s.waitProcess()
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return s.waitErr
}

// shutdownProcess closes stdin, waits briefly for the child to exit, and
// force-kills if the child hangs. Safe to call multiple times.
func (s *persistentSession) shutdownProcess() {
	if !s.isAlive() {
		return
	}
	_ = s.stdin.Close()
	done := s.waitProcess()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-done
	}
	s.markDead()
}

// ── Turn lifecycle ──

func (s *persistentSession) startTurn(timeout time.Duration) *turnState {
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	turn := &turnState{
		msgCh:       make(chan Message, 256),
		resCh:       make(chan Result, 1),
		startTime:   time.Now(),
		timeout:     timeout,
		finalStatus: "completed",
		done:        make(chan struct{}),
	}
	s.stateMu.Lock()
	s.turn = turn
	s.stateMu.Unlock()
	return turn
}

func (s *persistentSession) loadTurn() *turnState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.turn
}

func (s *persistentSession) clearTurn(t *turnState) {
	s.stateMu.Lock()
	if s.turn == t {
		s.turn = nil
	}
	s.stateMu.Unlock()
}

func (s *persistentSession) runTurn(ctx context.Context, prompt string, turn *turnState) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	defer close(turn.msgCh)
	defer close(turn.resCh)

	if err := s.writeUserMessage(prompt); err != nil {
		s.backend.cfg.Logger.Warn("claude-persistent: stdin write failed",
			"key", s.key, "error", err)
		s.failTurnAndKill(turn, "failed", fmt.Sprintf("stdin write failed: %v", err))
		return
	}

	turnCtx, cancel := context.WithTimeout(ctx, turn.timeout)
	defer cancel()

	select {
	case <-turn.done:
	case <-turnCtx.Done():
		status, errMsg := describeCtxErr(turnCtx.Err(), turn.timeout)
		s.failTurnAndKill(turn, status, errMsg)
		return
	case <-s.shutdown:
		if turn.finalStatus == "completed" {
			turn.finalStatus = "failed"
			turn.finalError = "claude process exited"
		}
	}

	s.clearTurn(turn)
	result := s.finalizeTurn(turn)
	turn.resCh <- result

	if s.key == "" {
		s.shutdownProcess()
	} else if result.Status == "completed" {
		s.scheduleIdleCleanup()
	} else {
		s.backend.evict(s.key, s)
		s.shutdownProcess()
	}
}

func (s *persistentSession) writeUserMessage(prompt string) error {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	_, err = s.stdin.Write(data)
	return err
}

func (s *persistentSession) failTurnAndKill(turn *turnState, status, errMsg string) {
	turn.finalStatus = status
	turn.finalError = errMsg
	s.clearTurn(turn)
	s.backend.evict(s.key, s)
	s.shutdownProcess()
	turn.resCh <- s.finalizeTurn(turn)
}

func (s *persistentSession) finalizeTurn(turn *turnState) Result {
	return Result{
		Status:     turn.finalStatus,
		Output:     turn.output.String(),
		Error:      turn.finalError,
		DurationMs: time.Since(turn.startTime).Milliseconds(),
		SessionID:  turn.sessionID,
	}
}

func describeCtxErr(err error, timeout time.Duration) (string, string) {
	switch err {
	case context.DeadlineExceeded:
		return "timeout", fmt.Sprintf("claude-persistent timed out after %s", timeout)
	case context.Canceled:
		return "aborted", "execution cancelled"
	default:
		if err != nil {
			return "failed", err.Error()
		}
		return "failed", "unknown context error"
	}
}

// ── Reader goroutine ──

func (s *persistentSession) reader() {
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg claudeSDKMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		s.dispatch(msg)
	}
	s.onStdoutClosed()
}

func (s *persistentSession) dispatch(msg claudeSDKMessage) {
	// control_request handling does not require an active turn.
	if msg.Type == "control_request" {
		s.handleControlRequest(msg)
		return
	}
	turn := s.loadTurn()
	if turn == nil {
		return
	}
	switch msg.Type {
	case "assistant":
		handleAssistantInto(msg, turn.msgCh, &turn.output)
	case "user":
		handleUserInto(msg, turn.msgCh)
	case "system":
		if msg.SessionID != "" {
			turn.sessionID = msg.SessionID
		}
		trySend(turn.msgCh, Message{Type: MessageStatus, Status: "running"})
	case "result":
		if msg.SessionID != "" {
			turn.sessionID = msg.SessionID
		}
		if msg.ResultText != "" {
			turn.output.Reset()
			turn.output.WriteString(msg.ResultText)
		}
		if msg.IsError {
			turn.finalStatus = "failed"
			turn.finalError = msg.ResultText
		}
		closeOnce(turn.done)
	case "log":
		if msg.Log != nil {
			trySend(turn.msgCh, Message{
				Type:    MessageLog,
				Level:   msg.Log.Level,
				Content: msg.Log.Message,
			})
		}
	}
}

func (s *persistentSession) handleControlRequest(msg claudeSDKMessage) {
	var req claudeControlRequestPayload
	if err := json.Unmarshal(msg.Request, &req); err != nil {
		return
	}
	var inputMap map[string]any
	if req.Input != nil {
		_ = json.Unmarshal(req.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = map[string]any{}
	}
	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": msg.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}
	data, err := json.Marshal(response)
	if err != nil {
		s.backend.cfg.Logger.Warn("claude-persistent: marshal control response failed", "error", err)
		return
	}
	data = append(data, '\n')
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if _, err := s.stdin.Write(data); err != nil {
		s.backend.cfg.Logger.Warn("claude-persistent: write control response failed", "error", err)
	}
}

func (s *persistentSession) onStdoutClosed() {
	turn := s.loadTurn()
	if turn != nil && turn.finalStatus == "completed" {
		turn.finalStatus = "failed"
		turn.finalError = "claude process exited"
	}
	if turn != nil {
		closeOnce(turn.done)
	}
}

// ── Monitor goroutine ──

func (s *persistentSession) monitor() {
	err := s.processWaitErr()
	s.markDead()
	s.backend.evict(s.key, s)
	if err != nil {
		s.backend.cfg.Logger.Info("claude-persistent process exited",
			"key", s.key, "error", err)
	} else {
		s.backend.cfg.Logger.Info("claude-persistent process exited", "key", s.key)
	}
	turn := s.loadTurn()
	if turn != nil && turn.finalStatus == "completed" {
		turn.finalStatus = "failed"
		turn.finalError = "claude process exited"
	}
	if turn != nil {
		closeOnce(turn.done)
	}
}

// ── Shared helpers ──

func handleAssistantInto(msg claudeSDKMessage, ch chan<- Message, output *strings.Builder) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}
	for _, block := range content.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   block.Name,
				CallID: block.ID,
				Input:  input,
			})
		}
	}
}

func handleUserInto(msg claudeSDKMessage, ch chan<- Message) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}
	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
}

func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}

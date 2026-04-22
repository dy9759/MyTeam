package agent

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Default idle timeout for pooled sessions. After this much time without a
// new turn, the long-lived claude process is shut down and removed from the
// pool so it doesn't hold resources forever.
const defaultClaudePersistentIdleTimeout = 10 * time.Minute

// claudePersistentBackend implements Backend by keeping a pool of long-lived
// `claude --input-format stream-json --output-format stream-json` processes
// keyed by ExecOptions.SessionKey, so multiple turns on the same conceptual
// session reuse a single child process and preserve its in-memory context.
//
// When SessionKey is empty, Execute spawns an ephemeral process that lives
// only for that turn. This lets callers opt in per-invocation without
// juggling two backend types.
type claudePersistentBackend struct {
	cfg         Config
	idleTimeout time.Duration

	mu       sync.RWMutex
	sessions map[string]*persistentSession
}

func newClaudePersistentBackend(cfg Config) *claudePersistentBackend {
	return &claudePersistentBackend{
		cfg:         cfg,
		idleTimeout: defaultClaudePersistentIdleTimeout,
		sessions:    map[string]*persistentSession{},
	}
}

// Execute runs one turn against a pooled or ephemeral claude session.
func (b *claudePersistentBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	session, err := b.getOrSpawn(opts, execPath)
	if err != nil {
		return nil, err
	}

	turn := session.startTurn(opts.Timeout)

	// Kick off the stdin write + per-turn lifecycle goroutine. We return a
	// Session immediately so the caller can range over Messages while we're
	// still writing the prompt.
	go session.runTurn(ctx, prompt, turn)

	return &Session{Messages: turn.msgCh, Result: turn.resCh}, nil
}

// getOrSpawn returns a live session for opts.SessionKey, spawning one on
// demand. Empty SessionKey always spawns a fresh ephemeral session.
func (b *claudePersistentBackend) getOrSpawn(opts ExecOptions, execPath string) (*persistentSession, error) {
	if opts.SessionKey == "" {
		return b.spawnSession("", opts, execPath)
	}

	b.mu.RLock()
	existing, ok := b.sessions[opts.SessionKey]
	b.mu.RUnlock()
	if ok && existing.isAlive() {
		existing.cancelIdleTimer()
		return existing, nil
	}

	b.mu.Lock()
	existing, ok = b.sessions[opts.SessionKey]
	if ok && existing.isAlive() {
		b.mu.Unlock()
		existing.cancelIdleTimer()
		return existing, nil
	}
	if ok && !existing.isAlive() {
		delete(b.sessions, opts.SessionKey)
	}

	session, err := b.spawnSession(opts.SessionKey, opts, execPath)
	if err != nil {
		b.mu.Unlock()
		return nil, err
	}
	b.sessions[opts.SessionKey] = session
	b.mu.Unlock()
	return session, nil
}

func (b *claudePersistentBackend) spawnSession(key string, opts ExecOptions, execPath string) (*persistentSession, error) {
	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}

	cmd := exec.Command(execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[claude-persistent:stderr] ")

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	b.cfg.Logger.Info("claude-persistent started",
		"pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model, "key", key)

	session := &persistentSession{
		backend:  b,
		key:      key,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		alive:    true,
		shutdown: make(chan struct{}),
		waitDone: make(chan struct{}),
	}

	go session.reader()
	go session.monitor()

	return session, nil
}

// evict removes a dead session from the pool if it is still the current
// entry for key. No-op for ephemeral (empty key) sessions.
func (b *claudePersistentBackend) evict(key string, expected *persistentSession) {
	if key == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.sessions[key]; ok && cur == expected {
		delete(b.sessions, key)
	}
}

// ── Test-only helpers (package-scoped) ──

// sessionCount returns the number of pooled sessions. Used by tests.
func (b *claudePersistentBackend) sessionCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sessions)
}

// sessionPID returns the PID of the pooled session for key, or (0, false)
// if not present. Used by tests.
func (b *claudePersistentBackend) sessionPID(key string) (int, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.sessions[key]
	if !ok || s.cmd == nil || s.cmd.Process == nil {
		return 0, false
	}
	return s.cmd.Process.Pid, true
}

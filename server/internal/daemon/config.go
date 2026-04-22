package daemon

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultServerURL             = "ws://localhost:8080/ws"
	DefaultPollInterval          = 3 * time.Second
	DefaultHeartbeatInterval     = 15 * time.Second
	DefaultAgentTimeout          = 2 * time.Hour
	DefaultRuntimeName           = "Local Agent"
	DefaultConfigReloadInterval  = 5 * time.Second
	DefaultWorkspaceSyncInterval = 30 * time.Second
	DefaultHealthPort            = 19514
	DefaultMaxConcurrentTasks    = 20

	// ClaudeModeSingle is the default. Each task spawns a fresh `claude` process.
	ClaudeModeSingle = "single"
	// ClaudeModePersistent reuses a long-lived `claude` process per (agent, issue)
	// across turns. Opt-in via MYTEAM_CLAUDE_MODE=persistent. On spawn failure
	// the daemon falls back to single-shot for that task.
	ClaudeModePersistent = "persistent"
)

// Config holds all daemon configuration.
type Config struct {
	ServerBaseURL      string
	DaemonID           string
	DeviceName         string
	RuntimeName        string
	CLIVersion         string                // myteam CLI version (e.g. "0.1.13")
	Profile            string                // profile name (empty = default)
	Agents             map[string]AgentEntry // "claude" -> entry, "codex" -> entry, "opencode" -> entry
	WorkspacesRoot     string                // base path for execution envs (default: ~/myteam_workspaces)
	KeepEnvAfterTask   bool                  // preserve env after task for debugging
	HealthPort         int                   // local HTTP port for health checks (default: 19514)
	MaxConcurrentTasks int                   // max tasks running in parallel (default: 20)
	ClaudeMode         string                // "single" (default) or "persistent"
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL          string
	WorkspacesRoot     string
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
	MaxConcurrentTasks int
	DaemonID           string
	DeviceName         string
	RuntimeName        string
	Profile            string // profile name (empty = default)
	HealthPort         int    // health check port (0 = use default)
}

// LoadConfig builds the daemon configuration from environment variables
// and optional CLI flag overrides.
func LoadConfig(overrides Overrides) (Config, error) {
	// Server URL: override > env > default
	rawServerURL := envOrDefault("MYTEAM_SERVER_URL", DefaultServerURL)
	if overrides.ServerURL != "" {
		rawServerURL = overrides.ServerURL
	}
	serverBaseURL, err := NormalizeServerBaseURL(rawServerURL)
	if err != nil {
		return Config{}, err
	}

	// Probe available agent CLIs
	agents := map[string]AgentEntry{}
	claudePath := envOrDefault("MYTEAM_CLAUDE_PATH", "claude")
	if _, err := exec.LookPath(claudePath); err == nil {
		agents["claude"] = AgentEntry{
			Path:  claudePath,
			Model: strings.TrimSpace(os.Getenv("MYTEAM_CLAUDE_MODEL")),
		}
	}
	codexPath := envOrDefault("MYTEAM_CODEX_PATH", "codex")
	if _, err := exec.LookPath(codexPath); err == nil {
		agents["codex"] = AgentEntry{
			Path:  codexPath,
			Model: strings.TrimSpace(os.Getenv("MYTEAM_CODEX_MODEL")),
		}
	}
	opencodePath := envOrDefault("MYTEAM_OPENCODE_PATH", "opencode")
	if _, err := exec.LookPath(opencodePath); err == nil {
		agents["opencode"] = AgentEntry{
			Path:  opencodePath,
			Model: strings.TrimSpace(os.Getenv("MYTEAM_OPENCODE_MODEL")),
		}
	}
	if len(agents) == 0 {
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codex, or opencode and ensure it is on PATH")
	}

	// Host info
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "local-machine"
	}

	// Durations: override > env > default
	pollInterval, err := durationFromEnv("MYTEAM_DAEMON_POLL_INTERVAL", DefaultPollInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.PollInterval > 0 {
		pollInterval = overrides.PollInterval
	}

	heartbeatInterval, err := durationFromEnv("MYTEAM_DAEMON_HEARTBEAT_INTERVAL", DefaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.HeartbeatInterval > 0 {
		heartbeatInterval = overrides.HeartbeatInterval
	}

	agentTimeout, err := durationFromEnv("MYTEAM_AGENT_TIMEOUT", DefaultAgentTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.AgentTimeout > 0 {
		agentTimeout = overrides.AgentTimeout
	}

	maxConcurrentTasks, err := intFromEnv("MYTEAM_DAEMON_MAX_CONCURRENT_TASKS", DefaultMaxConcurrentTasks)
	if err != nil {
		return Config{}, err
	}
	if overrides.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = overrides.MaxConcurrentTasks
	}

	// Profile
	profile := overrides.Profile

	// String overrides
	daemonID := envOrDefault("MYTEAM_DAEMON_ID", host)
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}
	// Suffix daemon ID with profile name to avoid collisions when multiple
	// daemons register against the same server.
	if profile != "" && !strings.HasSuffix(daemonID, "-"+profile) {
		daemonID = daemonID + "-" + profile
	}

	deviceName := envOrDefault("MYTEAM_DAEMON_DEVICE_NAME", host)
	if overrides.DeviceName != "" {
		deviceName = overrides.DeviceName
	}

	runtimeName := envOrDefault("MYTEAM_AGENT_RUNTIME_NAME", DefaultRuntimeName)
	if overrides.RuntimeName != "" {
		runtimeName = overrides.RuntimeName
	}

	// Workspaces root: override > env > default (~/myteam_workspaces or ~/myteam_workspaces_<profile>)
	workspacesRoot := strings.TrimSpace(os.Getenv("MYTEAM_WORKSPACES_ROOT"))
	if overrides.WorkspacesRoot != "" {
		workspacesRoot = overrides.WorkspacesRoot
	}
	if workspacesRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve home directory: %w (set MYTEAM_WORKSPACES_ROOT to override)", err)
		}
		if profile != "" {
			workspacesRoot = filepath.Join(home, "myteam_workspaces_"+profile)
		} else {
			workspacesRoot = filepath.Join(home, "myteam_workspaces")
		}
	}
	workspacesRoot, err = filepath.Abs(workspacesRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve absolute workspaces root: %w", err)
	}

	// Health port: override > default
	healthPort := DefaultHealthPort
	if overrides.HealthPort > 0 {
		healthPort = overrides.HealthPort
	}

	// Keep env after task: env > default (false)
	keepEnv := os.Getenv("MYTEAM_KEEP_ENV_AFTER_TASK") == "true" || os.Getenv("MYTEAM_KEEP_ENV_AFTER_TASK") == "1"

	// Claude execution mode: env > default ("single"). Unknown values fall
	// through to "single" so a typo never silently enables persistent mode.
	claudeMode := strings.ToLower(strings.TrimSpace(os.Getenv("MYTEAM_CLAUDE_MODE")))
	if claudeMode != ClaudeModePersistent {
		claudeMode = ClaudeModeSingle
	}

	return Config{
		ServerBaseURL:      serverBaseURL,
		DaemonID:           daemonID,
		DeviceName:         deviceName,
		RuntimeName:        runtimeName,
		Profile:            profile,
		Agents:             agents,
		WorkspacesRoot:     workspacesRoot,
		KeepEnvAfterTask:   keepEnv,
		HealthPort:         healthPort,
		MaxConcurrentTasks: maxConcurrentTasks,
		ClaudeMode:         claudeMode,
		PollInterval:       pollInterval,
		HeartbeatInterval:  heartbeatInterval,
		AgentTimeout:       agentTimeout,
	}, nil
}

// NormalizeServerBaseURL converts a WebSocket or HTTP URL to a base HTTP URL.
func NormalizeServerBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid MYTEAM_SERVER_URL: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("MYTEAM_SERVER_URL must use ws, wss, http, or https")
	}
	if u.Path == "/ws" {
		u.Path = ""
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

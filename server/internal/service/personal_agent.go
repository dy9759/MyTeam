package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CloudLLMConfig is the JSON shape stored in agent.cloud_llm_config.
// This is the server env snapshot at agent creation time.
type CloudLLMConfig struct {
	Kernel       string `json:"kernel,omitempty"`        // "openai_compat" (default) or "anthropic"
	BaseURL      string `json:"base_url,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// LoadCloudLLMConfigFromEnv reads the AGENT_* env vars and returns a CloudLLMConfig snapshot.
func LoadCloudLLMConfigFromEnv() CloudLLMConfig {
	return CloudLLMConfig{
		Kernel:       envOr("AGENT_KERNEL", "openai_compat"),
		BaseURL:      os.Getenv("AGENT_LLM_BASE_URL"),
		APIKey:       os.Getenv("AGENT_LLM_API_KEY"),
		Model:        os.Getenv("AGENT_LLM_MODEL"),
		SystemPrompt: os.Getenv("AGENT_SYSTEM_PROMPT"),
	}
}

// EnsurePersonalAgent creates a personal agent for the owner if one doesn't exist.
// It snapshots AGENT_* server env vars into runtime.metadata.cloud_llm_config so
// the auto-reply runner can read per-runtime config from the DB.
func EnsurePersonalAgent(ctx context.Context, queries *db.Queries, workspaceID, ownerID pgtype.UUID, userName string) (db.Agent, error) {
	existing, err := queries.GetPersonalAgent(ctx, db.GetPersonalAgentParams{
		WorkspaceID: workspaceID,
		OwnerID:     ownerID,
	})
	if err == nil {
		return existing, nil
	}

	runtime, err := queries.EnsureCloudRuntime(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("ensure cloud runtime: %w", err)
	}

	cfg := LoadCloudLLMConfigFromEnv()
	if cfg.APIKey == "" {
		slog.Warn("personal agent: AGENT_LLM_API_KEY not set; agent will be unable to reply until configured",
			"workspace_id", util.UUIDToString(workspaceID),
			"owner_id", util.UUIDToString(ownerID),
		)
	}

	agentName := userName + "'s Assistant"
	agent, err := queries.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: workspaceID,
		Name:        agentName,
		Description: "Personal AI assistant powered by Claude Agent SDK",
		RuntimeID:   runtime.ID,
		OwnerID:     ownerID,
	})
	if err != nil {
		return db.Agent{}, fmt.Errorf("create personal agent: %w", err)
	}

	// Snapshot the cloud LLM config to runtime.metadata so the cloud executor
	// and auto-reply path can pick it up.
	if configJSON, mErr := json.Marshal(cfg); mErr == nil {
		if sErr := queries.SetRuntimeMetadataKey(ctx, db.SetRuntimeMetadataKeyParams{
			ID:    runtime.ID,
			Key:   "cloud_llm_config",
			Value: configJSON,
		}); sErr != nil {
			slog.Warn("personal agent: persist runtime cloud_llm_config failed",
				"runtime_id", util.UUIDToString(runtime.ID),
				"error", sErr,
			)
		}
	}

	slog.Info("personal agent created",
		"agent_id", util.UUIDToString(agent.ID),
		"owner_id", util.UUIDToString(ownerID),
		"workspace_id", util.UUIDToString(workspaceID),
		"kernel", cfg.Kernel,
		"model", cfg.Model,
	)

	return agent, nil
}

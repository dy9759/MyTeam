package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
)

// IdentityGeneratorService generates identity cards for agents based on their
// profile, task history, skills, and workspace context.
type IdentityGeneratorService struct {
	Queries *db.Queries
}

// NewIdentityGeneratorService creates a new IdentityGeneratorService.
func NewIdentityGeneratorService(q *db.Queries) *IdentityGeneratorService {
	return &IdentityGeneratorService{Queries: q}
}

// GenerateCard generates an identity card for an agent based on:
// - Agent's existing profile and capabilities
// - Completed tasks (from agent_task_queue where status = 'completed')
// - Agent's skills
// - Workspace context
// Uses LLM (Anthropic API) to produce structured JSON.
// Returns the generated identity card as a map[string]any.
func (s *IdentityGeneratorService) GenerateCard(ctx context.Context, agentID, workspaceID string) (map[string]any, error) {
	// 1. Load agent profile.
	agent, err := s.Queries.GetAgent(ctx, util.ParseUUID(agentID))
	if err != nil {
		return nil, fmt.Errorf("load agent: %w", err)
	}

	// 2. Load agent's skills.
	skills, err := s.Queries.ListAgentSkills(ctx, agent.ID)
	if err != nil {
		slog.Debug("identity generator: failed to load skills", "agent_id", agentID, "error", err)
		skills = nil
	}

	// 3. Load completed tasks for context.
	tasks, err := s.Queries.ListAgentTasks(ctx, agent.ID)
	if err != nil {
		slog.Debug("identity generator: failed to load tasks", "agent_id", agentID, "error", err)
		tasks = nil
	}

	// Filter to completed tasks and build summary.
	var completedSummaries []string
	for _, t := range tasks {
		if t.Status == "completed" {
			completedSummaries = append(completedSummaries, fmt.Sprintf("Task %s (completed)", util.UUIDToString(t.ID)))
		}
	}

	// 4. Build skill names list.
	var skillNames []string
	for _, sk := range skills {
		skillNames = append(skillNames, sk.Name)
	}

	// 5. Build context for LLM.
	contextInfo := fmt.Sprintf(`Agent Name: %s
Description: %s
Instructions: %s
Capabilities: %s
Skills: %s
Completed Tasks Count: %d
Completed Task Summaries: %s`,
		agent.Name,
		agent.Description,
		agent.Instructions,
		strings.Join(agent.Capabilities, ", "),
		strings.Join(skillNames, ", "),
		len(completedSummaries),
		strings.Join(completedSummaries, "; "),
	)

	// 6. Call LLM to generate structured identity card.
	llmCfg := llmclient.FromEnv()
	if llmCfg.APIKey == "" {
		// Fallback: build a basic card without LLM.
		return buildBasicIdentityCard(agent, skillNames, completedSummaries), nil
	}

	prompt := fmt.Sprintf(`You are generating an identity card for an AI agent. Based on the following information, produce a structured identity card.

%s

Respond with JSON only:
{
  "capabilities": ["list of capabilities this agent has demonstrated"],
  "tools": ["list of tools the agent can use"],
  "skills": ["list of technical skills"],
  "subagents": [],
  "completed_projects": [],
  "description_auto": "A concise auto-generated description of this agent based on its history and capabilities"
}`, contextInfo)

	text, err := llmclient.New(llmCfg).Chat(ctx, "You are an agent profiler. Always respond with valid JSON matching the requested schema.", []llmclient.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		slog.Warn("identity generator: LLM call failed", "error", err)
		return buildBasicIdentityCard(agent, skillNames, completedSummaries), nil
	}

	var card map[string]any
	if err := json.Unmarshal([]byte(text), &card); err != nil {
		slog.Warn("identity generator: failed to parse LLM response", "error", err)
		return buildBasicIdentityCard(agent, skillNames, completedSummaries), nil
	}

	return card, nil
}

// buildBasicIdentityCard constructs a simple identity card without LLM.
func buildBasicIdentityCard(agent db.Agent, skills, completedTasks []string) map[string]any {
	card := map[string]any{
		"capabilities":       agent.Capabilities,
		"tools":              []string{},
		"skills":             skills,
		"subagents":          []string{},
		"completed_projects": []map[string]any{},
		"description_auto":   fmt.Sprintf("%s - %s", agent.Name, agent.Description),
	}
	if len(completedTasks) > 0 {
		card["description_auto"] = fmt.Sprintf("%s. Has completed %d tasks.", agent.Description, len(completedTasks))
	}
	return card
}

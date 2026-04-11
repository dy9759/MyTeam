package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
)

type PlanStep struct {
	Order            int      `json:"order"`
	Description      string   `json:"description"`
	RequiredSkills   []string `json:"required_skills"`
	EstimatedMinutes int      `json:"estimated_minutes"`
	DependsOn        []int    `json:"depends_on"`
	Parallelizable   bool     `json:"parallelizable"`
	AssignedAgentID  string   `json:"assigned_agent_id,omitempty"`
}

type GeneratedPlan struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Steps       []PlanStep `json:"steps"`
	Constraints string     `json:"constraints"`
	TaskBrief   string     `json:"task_brief,omitempty"`
}

// AgentIdentity holds info about an agent for plan generation.
type AgentIdentity struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Skills       []string `json:"skills"`
	Tools        []string `json:"tools"`
}

type PlanGeneratorService struct {
	Queries *db.Queries
}

func NewPlanGeneratorService(q *db.Queries) *PlanGeneratorService {
	return &PlanGeneratorService{Queries: q}
}

// GeneratePlanFromText uses LLM to parse natural language into structured plan.
func (s *PlanGeneratorService) GeneratePlanFromText(ctx context.Context, input string, workspaceID string) (*GeneratedPlan, error) {
	prompt := fmt.Sprintf(`You are a project planner. Parse the following request into a structured plan.

Request: %s

Respond with JSON only:
{
  "title": "short title",
  "description": "what needs to be done",
  "steps": [
    {"order": 1, "description": "step description", "required_skills": ["skill1"], "estimated_minutes": 30, "depends_on": [], "parallelizable": false}
  ],
  "constraints": "any constraints"
}`, input)

	llmCfg := llmclient.FromEnv()
	llmCfg.MaxTokens = 2048
	if llmCfg.APIKey == "" {
		// Fallback: simple parsing without LLM
		return &GeneratedPlan{
			Title:       truncate(input, 60),
			Description: input,
			Steps: []PlanStep{
				{Order: 1, Description: input, RequiredSkills: []string{}, Parallelizable: false},
			},
		}, nil
	}

	text, err := llmclient.New(llmCfg).Chat(ctx, "You are a project planning assistant. Always respond with valid JSON.", []llmclient.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		slog.Warn("LLM call failed", "error", err)
		return &GeneratedPlan{
			Title:       truncate(input, 60),
			Description: input,
			Steps:       []PlanStep{{Order: 1, Description: input}},
		}, nil
	}

	var plan GeneratedPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		slog.Warn("failed to parse LLM plan", "error", err)
		return &GeneratedPlan{Title: truncate(input, 60), Description: input, Steps: []PlanStep{{Order: 1, Description: input}}}, nil
	}

	return &plan, nil
}

// MatchAgentsToSteps finds best agents for each plan step based on capabilities.
func (s *PlanGeneratorService) MatchAgentsToSteps(ctx context.Context, steps []PlanStep, workspaceID string) (map[int][]string, error) {
	assignments := make(map[int][]string)

	for _, step := range steps {
		for _, skill := range step.RequiredSkills {
			agents, err := s.Queries.ListAgentsWithCapability(ctx, db.ListAgentsWithCapabilityParams{
				WorkspaceID:  util.ParseUUID(workspaceID),
				Capabilities: []string{skill},
			})
			if err != nil {
				continue
			}
			for _, a := range agents {
				assignments[step.Order] = append(assignments[step.Order], util.UUIDToString(a.ID))
			}
		}
	}

	return assignments, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// GeneratePlanWithContext generates a plan using conversation context and agent identities.
func (s *PlanGeneratorService) GeneratePlanWithContext(
	ctx context.Context,
	chatContext string,
	agents []AgentIdentity,
	workspaceID string,
) (*GeneratedPlan, error) {
	// Build agent descriptions for the prompt.
	var agentDescriptions strings.Builder
	for _, a := range agents {
		fmt.Fprintf(&agentDescriptions, "- Agent %q (ID: %s)\n", a.Name, a.ID)
		if len(a.Capabilities) > 0 {
			fmt.Fprintf(&agentDescriptions, "  Capabilities: %s\n", strings.Join(a.Capabilities, ", "))
		}
		if len(a.Skills) > 0 {
			fmt.Fprintf(&agentDescriptions, "  Skills: %s\n", strings.Join(a.Skills, ", "))
		}
		if len(a.Tools) > 0 {
			fmt.Fprintf(&agentDescriptions, "  Tools: %s\n", strings.Join(a.Tools, ", "))
		}
	}

	prompt := fmt.Sprintf(`You are a project planner. Based on the following conversation context and available agents, create a structured project plan.

## Conversation Context
%s

## Available Agents
%s

Create a plan that:
1. Breaks the work into clear steps
2. Assigns each step to the most appropriate agent based on their capabilities and skills
3. Defines dependencies between steps
4. Identifies which steps can run in parallel
5. Generates a task brief summarizing the objective

Respond with JSON only:
{
  "title": "short project title",
  "description": "what needs to be done",
  "steps": [
    {
      "order": 1,
      "description": "step description",
      "required_skills": ["skill1"],
      "estimated_minutes": 30,
      "depends_on": [],
      "assigned_agent_id": "agent-uuid",
      "parallelizable": false
    }
  ],
  "constraints": "any constraints or risks",
  "task_brief": "structured task brief with objective, scope, and success criteria"
}`, truncate(chatContext, 8000), agentDescriptions.String())

	llmCfg := llmclient.FromEnv()
	llmCfg.MaxTokens = 4096
	if llmCfg.APIKey == "" {
		return s.fallbackPlanWithContext(chatContext, agents), nil
	}

	text, err := llmclient.New(llmCfg).Chat(ctx, "You are a project planning assistant. Always respond with valid JSON. Assign agents to steps based on their capabilities.", []llmclient.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		slog.Warn("LLM call failed for plan with context", "error", err)
		return s.fallbackPlanWithContext(chatContext, agents), nil
	}

	var plan GeneratedPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		slog.Warn("failed to parse LLM plan with context", "error", err)
		return s.fallbackPlanWithContext(chatContext, agents), nil
	}

	return &plan, nil
}

// fallbackPlanWithContext creates a simple single-step plan when LLM is unavailable.
func (s *PlanGeneratorService) fallbackPlanWithContext(chatContext string, agents []AgentIdentity) *GeneratedPlan {
	var assignedAgent string
	if len(agents) > 0 {
		assignedAgent = agents[0].ID
	}
	return &GeneratedPlan{
		Title:       truncate(chatContext, 60),
		Description: chatContext,
		Steps: []PlanStep{
			{
				Order:           1,
				Description:     chatContext,
				RequiredSkills:  []string{},
				Parallelizable:  false,
				AssignedAgentID: assignedAgent,
			},
		},
		TaskBrief: fmt.Sprintf("## Objective\n%s\n", truncate(chatContext, 200)),
	}
}

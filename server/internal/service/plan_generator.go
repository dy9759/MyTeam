// Package service: plan_generator.go — PlanGeneratorService converts a
// natural-language request (or a chat thread) into a draft project plan
// shaped for the Plan 5 Task + ParticipantSlot model.
//
// Output (GeneratePlanResult):
//
//	Plan       — title / description / constraints / task_brief
//	Tasks      — one TaskDraft per planned step, each with a LocalID
//	             ("T1", "T2", …) used to wire DependsOn between tasks
//	             before any DB UUIDs exist.
//	Slots      — one SlotDraft per ParticipantSlot, referencing its
//	             owning Task by TaskLocalID.
//	Warnings   — non-fatal validation findings (DAG cycle, malformed
//	             LLM JSON, skill coverage gap, collab-mode mismatch).
//	             Generation never returns an error for these — callers
//	             surface them to the user but still get usable drafts.
//	TokenUsage — placeholder; populated when the underlying llmclient
//	             starts surfacing token counts.
//
// The handler (project_create.go) is responsible for INSERTing Task /
// Slot rows from these drafts, translating LocalIDs to UUIDs, and
// invoking UpdateTaskDependsOn after all tasks are persisted.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
)

// GeneratePlanResult is the unified return type of every PlanGenerator
// entrypoint. See package doc.
type GeneratePlanResult struct {
	Plan       PlanDraft   `json:"plan"`
	Tasks      []TaskDraft `json:"tasks"`
	Slots      []SlotDraft `json:"slots"`
	Warnings   []string    `json:"warnings"`
	TokenUsage TokenUsage  `json:"token_usage"`
}

// PlanDraft is the plan-level description (title + brief + constraints).
type PlanDraft struct {
	Title          string `json:"title"`
	TaskBrief      string `json:"task_brief,omitempty"`
	Description    string `json:"description,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	Constraints    string `json:"constraints,omitempty"`
}

// TaskDraft is one task in the plan. LocalID is a draft-time identifier
// ("T1"…"Tn") used only for wiring DependsOn / SlotDraft.TaskLocalID
// before the task receives a real UUID at INSERT time.
type TaskDraft struct {
	LocalID                string   `json:"local_id"`
	Title                  string   `json:"title"`
	Description            string   `json:"description,omitempty"`
	StepOrder              int      `json:"step_order"`
	DependsOnLocal         []string `json:"depends_on,omitempty"`
	PrimaryAssigneeAgentID string   `json:"primary_assignee_agent_id,omitempty"`
	FallbackAgentIDs       []string `json:"fallback_agent_ids,omitempty"`
	RequiredSkills         []string `json:"required_skills,omitempty"`
	CollaborationMode      string   `json:"collaboration_mode,omitempty"`
	AcceptanceCriteria     string   `json:"acceptance_criteria,omitempty"`
}

// SlotDraft is one ParticipantSlot belonging to a TaskDraft (referenced
// by TaskLocalID). Mirrors columns on participant_slot.
type SlotDraft struct {
	TaskLocalID     string `json:"task_local_id"`
	SlotType        string `json:"slot_type"`
	SlotOrder       int    `json:"slot_order"`
	ParticipantID   string `json:"participant_id,omitempty"`
	ParticipantType string `json:"participant_type,omitempty"`
	Trigger         string `json:"trigger"`
	Blocking        bool   `json:"blocking"`
	Required        bool   `json:"required"`
	Responsibility  string `json:"responsibility,omitempty"`
	ExpectedOutput  string `json:"expected_output,omitempty"`
}

// TokenUsage holds LLM token counts. Currently always zero; reserved for
// when llmclient surfaces usage on each Chat call.
type TokenUsage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// AgentIdentity holds info about an agent for plan generation.
type AgentIdentity struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Skills       []string `json:"skills"`
	Tools        []string `json:"tools"`
}

// Warning code constants — emitted into GeneratePlanResult.Warnings so
// callers can branch / display per code rather than parsing free text.
const (
	WarnPlanGenMalformed   = "PLAN_GEN_MALFORMED"
	WarnDAGCycle           = "DAG_CYCLE"
	WarnSlotMissingTask    = "SLOT_TASK_LOCAL_ID_MISSING"
	WarnCollabModeMismatch = "COLLAB_MODE_SLOT_MISMATCH"
	WarnSkillCoverageGap   = "SKILL_COVERAGE_GAP"
	WarnLLMUnavailable     = "LLM_UNAVAILABLE"
)

// llmGeneratedPlan mirrors the JSON shape we ask the LLM for. It nests
// Slots inside each Task to keep the prompt simple; we flatten into
// TaskDraft + SlotDraft after parsing.
type llmGeneratedPlan struct {
	Plan  PlanDraft         `json:"plan"`
	Tasks []llmGeneratedTask `json:"tasks"`
}

type llmGeneratedTask struct {
	LocalID                string             `json:"local_id"`
	Title                  string             `json:"title"`
	Description            string             `json:"description"`
	StepOrder              int                `json:"step_order"`
	DependsOnLocal         []string           `json:"depends_on"`
	PrimaryAssigneeAgentID string             `json:"primary_assignee_agent_id"`
	FallbackAgentIDs       []string           `json:"fallback_agent_ids"`
	RequiredSkills         []string           `json:"required_skills"`
	CollaborationMode      string             `json:"collaboration_mode"`
	AcceptanceCriteria     string             `json:"acceptance_criteria"`
	Slots                  []llmGeneratedSlot `json:"slots"`
}

type llmGeneratedSlot struct {
	SlotType        string `json:"slot_type"`
	SlotOrder       int    `json:"slot_order"`
	ParticipantID   string `json:"participant_id"`
	ParticipantType string `json:"participant_type"`
	Trigger         string `json:"trigger"`
	Blocking        *bool  `json:"blocking"`
	Required        *bool  `json:"required"`
	Responsibility  string `json:"responsibility"`
	ExpectedOutput  string `json:"expected_output"`
}

type PlanGeneratorService struct {
	Queries *db.Queries
}

func NewPlanGeneratorService(q *db.Queries) *PlanGeneratorService {
	return &PlanGeneratorService{Queries: q}
}

// GeneratePlanFromText parses a natural-language request into a Task +
// Slot draft. workspaceID is currently informational; it will be used to
// look up workspace-scoped agents/skills in a follow-up.
func (s *PlanGeneratorService) GeneratePlanFromText(ctx context.Context, input, workspaceID string) (*GeneratePlanResult, error) {
	prompt := buildTextPrompt(input)
	return s.runLLMOrFallback(ctx, prompt, input, nil, workspaceID)
}

// GeneratePlanWithContext generates a plan using conversation context and
// the supplied agent identities. The agents are exposed to the LLM so it
// can assign primary_assignee_agent_id from a real candidate set.
func (s *PlanGeneratorService) GeneratePlanWithContext(
	ctx context.Context,
	chatContext string,
	agents []AgentIdentity,
	workspaceID string,
) (*GeneratePlanResult, error) {
	prompt := buildContextPrompt(chatContext, agents)
	return s.runLLMOrFallback(ctx, prompt, chatContext, agents, workspaceID)
}

// runLLMOrFallback calls the LLM (when configured), parses the response,
// validates it, and returns a GeneratePlanResult. When the LLM is
// unavailable or returns malformed JSON we fall back to a minimal
// single-task plan with a default agent_execution + human_review slot
// pair so downstream code (project_create.go) always has something to
// materialize.
func (s *PlanGeneratorService) runLLMOrFallback(
	ctx context.Context,
	prompt, fallbackInput string,
	agents []AgentIdentity,
	workspaceID string,
) (*GeneratePlanResult, error) {
	llmCfg := llmclient.FromEnv()
	llmCfg.MaxTokens = 4096

	if llmCfg.APIKey == "" {
		res := fallbackPlan(fallbackInput, agents)
		res.Warnings = append(res.Warnings, WarnLLMUnavailable)
		return s.validate(res, agents), nil
	}

	text, err := llmclient.New(llmCfg).Chat(
		ctx,
		"You are a project planning assistant. Always respond with valid JSON matching the requested schema. Never include any prose outside the JSON.",
		[]llmclient.Message{{Role: "user", Content: prompt}},
	)
	if err != nil {
		slog.Warn("plan generator: LLM call failed", "error", err)
		res := fallbackPlan(fallbackInput, agents)
		res.Warnings = append(res.Warnings, WarnLLMUnavailable)
		return s.validate(res, agents), nil
	}

	res, parseWarn := parseLLMResponse(text, fallbackInput, agents)
	return s.validate(res, agents).appendWarnings(parseWarn...), nil
}

// parseLLMResponse extracts an llmGeneratedPlan from the LLM's text and
// flattens it into TaskDraft + SlotDraft slices. On JSON failure it
// returns the fallback plan and a PLAN_GEN_MALFORMED warning so the
// caller can still proceed.
func parseLLMResponse(text, fallbackInput string, agents []AgentIdentity) (*GeneratePlanResult, []string) {
	jsonText := extractJSONObject(text)
	var raw llmGeneratedPlan
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		slog.Warn("plan generator: failed to parse LLM JSON", "error", err)
		return fallbackPlan(fallbackInput, agents), []string{WarnPlanGenMalformed}
	}

	res := &GeneratePlanResult{Plan: raw.Plan}

	// Ensure every task has a LocalID — generate "T<n>" if missing so
	// downstream wiring still works.
	for i, t := range raw.Tasks {
		localID := strings.TrimSpace(t.LocalID)
		if localID == "" {
			localID = fmt.Sprintf("T%d", i+1)
		}
		stepOrder := t.StepOrder
		if stepOrder == 0 {
			stepOrder = i + 1
		}
		mode := strings.TrimSpace(t.CollaborationMode)
		if mode == "" {
			mode = "agent_exec_human_review"
		}
		res.Tasks = append(res.Tasks, TaskDraft{
			LocalID:                localID,
			Title:                  strings.TrimSpace(t.Title),
			Description:            t.Description,
			StepOrder:              stepOrder,
			DependsOnLocal:         t.DependsOnLocal,
			PrimaryAssigneeAgentID: t.PrimaryAssigneeAgentID,
			FallbackAgentIDs:       t.FallbackAgentIDs,
			RequiredSkills:         t.RequiredSkills,
			CollaborationMode:      mode,
			AcceptanceCriteria:     t.AcceptanceCriteria,
		})

		for j, sl := range t.Slots {
			slotOrder := sl.SlotOrder
			if slotOrder == 0 {
				slotOrder = j + 1
			}
			trigger := strings.TrimSpace(sl.Trigger)
			if trigger == "" {
				trigger = SlotTriggerDuringExecution
			}
			res.Slots = append(res.Slots, SlotDraft{
				TaskLocalID:     localID,
				SlotType:        strings.TrimSpace(sl.SlotType),
				SlotOrder:       slotOrder,
				ParticipantID:   sl.ParticipantID,
				ParticipantType: sl.ParticipantType,
				Trigger:         trigger,
				Blocking:        boolPtrOrDefault(sl.Blocking, true),
				Required:        boolPtrOrDefault(sl.Required, true),
				Responsibility:  sl.Responsibility,
				ExpectedOutput:  sl.ExpectedOutput,
			})
		}
	}

	if res.Plan.Title == "" {
		res.Plan.Title = truncate(fallbackInput, 60)
	}

	return res, nil
}

// validate runs soft validation on the result and appends warnings.
// It never strips tasks/slots — the goal is "always have something
// usable, but tell the caller what looks off".
func (s *PlanGeneratorService) validate(res *GeneratePlanResult, agents []AgentIdentity) *GeneratePlanResult {
	if res == nil {
		return res
	}
	res.Warnings = appendUnique(res.Warnings, validateSlotTaskRefs(res.Tasks, res.Slots)...)
	res.Warnings = appendUnique(res.Warnings, validateDAG(res.Tasks)...)
	res.Warnings = appendUnique(res.Warnings, validateCollabModeSlotComposition(res.Tasks, res.Slots)...)
	res.Warnings = appendUnique(res.Warnings, validateSkillCoverage(res.Tasks, agents)...)
	return res
}

// validateSlotTaskRefs flags any SlotDraft whose TaskLocalID doesn't
// match a TaskDraft.LocalID. We can't emit these slots without a parent
// task, so the warning is surfaced and the materializer will skip them.
func validateSlotTaskRefs(tasks []TaskDraft, slots []SlotDraft) []string {
	taskIDs := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskIDs[t.LocalID] = struct{}{}
	}
	var bad []string
	for _, sl := range slots {
		if _, ok := taskIDs[sl.TaskLocalID]; !ok {
			bad = append(bad, sl.TaskLocalID)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return []string{fmt.Sprintf("%s:%s", WarnSlotMissingTask, strings.Join(uniqueStrings(bad), ","))}
}

// validateDAG runs a DFS over the depends_on graph and reports any
// cycle as "DAG_CYCLE:T1->T2->T3->T1". Returns nil when acyclic.
func validateDAG(tasks []TaskDraft) []string {
	if len(tasks) == 0 {
		return nil
	}
	deps := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		deps[t.LocalID] = t.DependsOnLocal
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(tasks))
	var stack []string
	var cyclePath []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		stack = append(stack, node)
		for _, dep := range deps[node] {
			switch color[dep] {
			case gray:
				// found a cycle: from dep down the stack
				idx := -1
				for i, v := range stack {
					if v == dep {
						idx = i
						break
					}
				}
				if idx >= 0 {
					cyclePath = append([]string{}, stack[idx:]...)
					cyclePath = append(cyclePath, dep)
				}
				return true
			case white:
				if dfs(dep) {
					return true
				}
			}
		}
		color[node] = black
		stack = stack[:len(stack)-1]
		return false
	}

	// Iterate in stable order so the reported cycle is deterministic.
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if color[k] == white {
			stack = stack[:0]
			if dfs(k) {
				return []string{fmt.Sprintf("%s:%s", WarnDAGCycle, strings.Join(cyclePath, "->"))}
			}
		}
	}
	return nil
}

// validateCollabModeSlotComposition checks each task's slots against
// its collaboration_mode.
//
//	agent_exec_human_review  → requires agent_execution and human_review
//	human_input_agent_exec   → requires human_input and agent_execution
//	agent_prepare_human_action → requires agent_execution
//	mixed                    → no requirement (any composition allowed)
//
// Missing slot kinds are reported as one COLLAB_MODE_SLOT_MISMATCH
// warning per task with the missing slot type list appended.
func validateCollabModeSlotComposition(tasks []TaskDraft, slots []SlotDraft) []string {
	slotsByTask := make(map[string]map[string]struct{}, len(tasks))
	for _, sl := range slots {
		set, ok := slotsByTask[sl.TaskLocalID]
		if !ok {
			set = make(map[string]struct{})
			slotsByTask[sl.TaskLocalID] = set
		}
		set[sl.SlotType] = struct{}{}
	}

	var warnings []string
	for _, t := range tasks {
		want := requiredSlotsForMode(t.CollaborationMode)
		if len(want) == 0 {
			continue
		}
		got := slotsByTask[t.LocalID]
		var missing []string
		for _, w := range want {
			if _, ok := got[w]; !ok {
				missing = append(missing, w)
			}
		}
		if len(missing) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%s:%s missing %s for mode %s",
				WarnCollabModeMismatch, t.LocalID, strings.Join(missing, ","), t.CollaborationMode,
			))
		}
	}
	return warnings
}

// requiredSlotsForMode returns the slot_type set every task in this
// collaboration mode is expected to declare. Returns nil for modes
// without a requirement (e.g. "mixed").
func requiredSlotsForMode(mode string) []string {
	switch mode {
	case "agent_exec_human_review":
		return []string{SlotTypeAgentExecution, SlotTypeHumanReview}
	case "human_input_agent_exec":
		return []string{SlotTypeHumanInput, SlotTypeAgentExecution}
	case "agent_prepare_human_action":
		return []string{SlotTypeAgentExecution}
	case "mixed":
		return nil
	default:
		// Unknown mode is treated as 'no requirement' — the schema CHECK
		// constraint will reject it at INSERT time anyway.
		return nil
	}
}

// validateSkillCoverage flags skills that no candidate agent has in
// their identity_card. Empty agent list is treated as "no info" and
// produces no warnings (we can't know what's covered).
func validateSkillCoverage(tasks []TaskDraft, agents []AgentIdentity) []string {
	if len(agents) == 0 {
		return nil
	}
	have := make(map[string]struct{})
	for _, a := range agents {
		for _, s := range a.Skills {
			have[s] = struct{}{}
		}
		for _, c := range a.Capabilities {
			have[c] = struct{}{}
		}
	}
	uncovered := make(map[string]struct{})
	for _, t := range tasks {
		for _, sk := range t.RequiredSkills {
			if _, ok := have[sk]; !ok {
				uncovered[sk] = struct{}{}
			}
		}
	}
	if len(uncovered) == 0 {
		return nil
	}
	out := make([]string, 0, len(uncovered))
	for s := range uncovered {
		out = append(out, s)
	}
	sort.Strings(out)
	return []string{fmt.Sprintf("%s:%s", WarnSkillCoverageGap, strings.Join(out, ","))}
}

// fallbackPlan returns a minimal one-task plan with default
// agent_execution + human_review slots. Used whenever LLM is unavailable
// or its response can't be parsed.
func fallbackPlan(input string, agents []AgentIdentity) *GeneratePlanResult {
	var assignedAgent string
	if len(agents) > 0 {
		assignedAgent = agents[0].ID
	}

	task := TaskDraft{
		LocalID:                "T1",
		Title:                  truncate(input, 80),
		Description:            input,
		StepOrder:              1,
		PrimaryAssigneeAgentID: assignedAgent,
		RequiredSkills:         []string{},
		CollaborationMode:      "agent_exec_human_review",
		AcceptanceCriteria:     "",
	}
	slots := []SlotDraft{
		{
			TaskLocalID:     "T1",
			SlotType:        SlotTypeAgentExecution,
			SlotOrder:       1,
			ParticipantID:   assignedAgent,
			ParticipantType: "agent",
			Trigger:         SlotTriggerDuringExecution,
			Blocking:        true,
			Required:        true,
		},
		{
			TaskLocalID:     "T1",
			SlotType:        SlotTypeHumanReview,
			SlotOrder:       2,
			ParticipantType: "member",
			Trigger:         SlotTriggerBeforeDone,
			Blocking:        true,
			Required:        true,
		},
	}

	return &GeneratePlanResult{
		Plan: PlanDraft{
			Title:       truncate(input, 60),
			Description: input,
			TaskBrief:   fmt.Sprintf("## Objective\n%s\n", truncate(input, 200)),
		},
		Tasks: []TaskDraft{task},
		Slots: slots,
	}
}

// buildTextPrompt is the prompt for plain natural-language input (no
// agent context). Asks the LLM for the same JSON schema we use with
// chat context, just without the "## Available Agents" block.
func buildTextPrompt(input string) string {
	return fmt.Sprintf(`You are a project planner. Parse the following request into a structured plan with discrete tasks and human/agent collaboration slots.

## Request
%s

## Output JSON Schema
{
  "plan": {
    "title": "short title",
    "task_brief": "structured brief: objective, scope, success criteria",
    "description": "what needs to be done",
    "expected_output": "deliverable shape",
    "constraints": "any constraints or risks"
  },
  "tasks": [
    {
      "local_id": "T1",
      "title": "task title",
      "description": "what the task does",
      "step_order": 1,
      "depends_on": [],
      "primary_assignee_agent_id": "",
      "fallback_agent_ids": [],
      "required_skills": ["skill1"],
      "collaboration_mode": "agent_exec_human_review",
      "acceptance_criteria": "how to know it's done",
      "slots": [
        {
          "slot_type": "agent_execution",
          "slot_order": 1,
          "participant_type": "agent",
          "trigger": "during_execution",
          "blocking": true,
          "required": true,
          "responsibility": "do the work",
          "expected_output": "deliverable"
        },
        {
          "slot_type": "human_review",
          "slot_order": 2,
          "participant_type": "member",
          "trigger": "before_done",
          "blocking": true,
          "required": true,
          "responsibility": "approve before completion",
          "expected_output": "approval"
        }
      ]
    }
  ]
}

Rules:
- collaboration_mode is one of: agent_exec_human_review, human_input_agent_exec, agent_prepare_human_action, mixed.
- slot_type is one of: human_input, agent_execution, human_review.
- trigger is one of: before_execution, during_execution, before_done.
- depends_on lists local_id values of upstream tasks; never reference the task itself or downstream tasks.
- Respond with JSON only, no markdown fences, no commentary.`, input)
}

// buildContextPrompt is the prompt for plan generation from chat
// context with a known agent set. The agent list is rendered so the
// LLM can pick primary_assignee_agent_id from real IDs.
func buildContextPrompt(chatContext string, agents []AgentIdentity) string {
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

	return fmt.Sprintf(`You are a project planner. Based on the conversation context and the available agents below, produce a structured project plan with tasks and human/agent collaboration slots.

## Conversation Context
%s

## Available Agents
%s

## Output JSON Schema
{
  "plan": {
    "title": "short project title",
    "task_brief": "objective + scope + success criteria",
    "description": "what needs to be done",
    "expected_output": "deliverable shape",
    "constraints": "any constraints or risks"
  },
  "tasks": [
    {
      "local_id": "T1",
      "title": "task title",
      "description": "what the task does",
      "step_order": 1,
      "depends_on": [],
      "primary_assignee_agent_id": "<one of the agent IDs above>",
      "fallback_agent_ids": [],
      "required_skills": ["skill1"],
      "collaboration_mode": "agent_exec_human_review",
      "acceptance_criteria": "how to know it's done",
      "slots": [
        {
          "slot_type": "agent_execution",
          "slot_order": 1,
          "participant_id": "<the assigned agent ID>",
          "participant_type": "agent",
          "trigger": "during_execution",
          "blocking": true,
          "required": true,
          "responsibility": "do the work",
          "expected_output": "deliverable"
        },
        {
          "slot_type": "human_review",
          "slot_order": 2,
          "participant_type": "member",
          "trigger": "before_done",
          "blocking": true,
          "required": true,
          "responsibility": "approve",
          "expected_output": "approval"
        }
      ]
    }
  ]
}

Rules:
- Each task's primary_assignee_agent_id must be one of the agent IDs above (or empty if none fits).
- collaboration_mode ∈ {agent_exec_human_review, human_input_agent_exec, agent_prepare_human_action, mixed}.
- slot_type ∈ {human_input, agent_execution, human_review}; trigger ∈ {before_execution, during_execution, before_done}.
- depends_on lists local_id values of upstream tasks (never the same task or downstream tasks).
- Respond with JSON only.`, truncate(chatContext, 8000), agentDescriptions.String())
}

// MatchAgentsToSkills returns, for each TaskDraft's RequiredSkills, the
// list of agent IDs from the supplied workspace whose identity_card
// covers at least one of those skills. This is a thin helper retained
// for callers that want to enrich drafts before persistence.
//
// Replaces the old MatchAgentsToSteps which operated on []PlanStep.
func (s *PlanGeneratorService) MatchAgentsToSkills(ctx context.Context, tasks []TaskDraft, workspaceID string) (map[string][]string, error) {
	assignments := make(map[string][]string)

	agents, err := s.Queries.ListAgents(ctx, util.ParseUUID(workspaceID))
	if err != nil {
		return assignments, nil
	}

	type agentCaps struct {
		id   string
		caps map[string]struct{}
	}
	agentIndex := make([]agentCaps, 0, len(agents))
	for _, a := range agents {
		caps := capabilitiesFromIdentityCard(a.IdentityCard)
		set := make(map[string]struct{}, len(caps))
		for _, c := range caps {
			set[c] = struct{}{}
		}
		agentIndex = append(agentIndex, agentCaps{id: util.UUIDToString(a.ID), caps: set})
	}

	for _, t := range tasks {
		for _, skill := range t.RequiredSkills {
			for _, ag := range agentIndex {
				if _, ok := ag.caps[skill]; ok {
					assignments[t.LocalID] = append(assignments[t.LocalID], ag.id)
				}
			}
		}
	}

	return assignments, nil
}

// --- internal helpers ---

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// extractJSONObject finds the first balanced {...} block in s and
// returns it. Some LLMs wrap their JSON in markdown fences or chatty
// preamble; this strips those without us having to special-case each
// failure mode.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// strip leading fence (```json or ```)
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// boolPtrOrDefault returns *p when set, otherwise defaultValue. Used
// for slot.blocking / slot.required where the LLM may legitimately omit
// the field and we want our default to win.
func boolPtrOrDefault(p *bool, defaultValue bool) bool {
	if p == nil {
		return defaultValue
	}
	return *p
}

// uniqueStrings returns ss with duplicates dropped, preserving order
// (first occurrence wins).
func uniqueStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// appendUnique appends only items not already present in dst.
// Stable wrt input order.
func appendUnique(dst []string, items ...string) []string {
	if len(items) == 0 {
		return dst
	}
	have := make(map[string]struct{}, len(dst))
	for _, d := range dst {
		have[d] = struct{}{}
	}
	for _, it := range items {
		if _, ok := have[it]; ok {
			continue
		}
		have[it] = struct{}{}
		dst = append(dst, it)
	}
	return dst
}

// appendWarnings is a method-style helper that tolerates a nil receiver
// (returns the same nil) so chained calls stay one-liners.
func (r *GeneratePlanResult) appendWarnings(ws ...string) *GeneratePlanResult {
	if r == nil || len(ws) == 0 {
		return r
	}
	r.Warnings = appendUnique(r.Warnings, ws...)
	return r
}

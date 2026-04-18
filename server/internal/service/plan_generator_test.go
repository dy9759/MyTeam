package service

import (
	"strings"
	"testing"
)

// TestParseLLMResponse_ValidJSON checks the parser maps a well-formed
// LLM response into TaskDraft + SlotDraft slices, defaults
// collaboration_mode to agent_exec_human_review, and reports no
// warnings on the parse path.
func TestParseLLMResponse_ValidJSON(t *testing.T) {
	resp := `{
        "plan": {
          "title": "Ship feature X",
          "task_brief": "## Objective\n…",
          "description": "Build X",
          "constraints": "by friday"
        },
        "tasks": [
          {
            "local_id": "T1",
            "title": "Design",
            "description": "Sketch the UI",
            "step_order": 1,
            "depends_on": [],
            "primary_assignee_agent_id": "agent-a",
            "required_skills": ["design"],
            "collaboration_mode": "agent_exec_human_review",
            "slots": [
              {"slot_type":"agent_execution","slot_order":1,"participant_id":"agent-a","participant_type":"agent","trigger":"during_execution","blocking":true,"required":true},
              {"slot_type":"human_review","slot_order":2,"participant_type":"member","trigger":"before_done","blocking":true,"required":true}
            ]
          },
          {
            "local_id": "T2",
            "title": "Build",
            "step_order": 2,
            "depends_on": ["T1"],
            "primary_assignee_agent_id": "agent-b",
            "required_skills": ["coding"],
            "slots": [
              {"slot_type":"agent_execution","slot_order":1,"participant_id":"agent-b","participant_type":"agent","trigger":"during_execution"},
              {"slot_type":"human_review","slot_order":2,"participant_type":"member","trigger":"before_done"}
            ]
          }
        ]
      }`

	res, parseWarn := parseLLMResponse(resp, "fallback", nil)
	if len(parseWarn) != 0 {
		t.Fatalf("expected no parse warnings, got %v", parseWarn)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Plan.Title != "Ship feature X" {
		t.Errorf("plan.title = %q", res.Plan.Title)
	}
	if len(res.Tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(res.Tasks))
	}
	if res.Tasks[1].LocalID != "T2" {
		t.Errorf("task[1].LocalID = %q", res.Tasks[1].LocalID)
	}
	if got := res.Tasks[1].DependsOnLocal; len(got) != 1 || got[0] != "T1" {
		t.Errorf("task[1].DependsOnLocal = %v", got)
	}
	if res.Tasks[1].CollaborationMode != "agent_exec_human_review" {
		t.Errorf("task[1].CollaborationMode default = %q", res.Tasks[1].CollaborationMode)
	}
	if len(res.Slots) != 4 {
		t.Fatalf("want 4 slots, got %d", len(res.Slots))
	}
	for _, sl := range res.Slots {
		if sl.TaskLocalID == "" || sl.SlotType == "" || sl.Trigger == "" {
			t.Errorf("incomplete slot: %+v", sl)
		}
	}
	// blocking/required default true when omitted (T2 slots omit them)
	for _, sl := range res.Slots {
		if sl.TaskLocalID == "T2" && !sl.Blocking {
			t.Errorf("T2 slot blocking should default true: %+v", sl)
		}
	}
}

// TestParseLLMResponse_Malformed exercises the JSON failure path: the
// parser should return the fallback plan and a PLAN_GEN_MALFORMED
// warning, never an error.
func TestParseLLMResponse_Malformed(t *testing.T) {
	res, warn := parseLLMResponse("not json at all", "fallback input", nil)
	if res == nil {
		t.Fatal("nil result on malformed input")
	}
	if len(warn) != 1 || warn[0] != WarnPlanGenMalformed {
		t.Errorf("want [%s], got %v", WarnPlanGenMalformed, warn)
	}
	if len(res.Tasks) != 1 {
		t.Errorf("fallback should have 1 task, got %d", len(res.Tasks))
	}
	if res.Tasks[0].LocalID != "T1" {
		t.Errorf("fallback LocalID = %q", res.Tasks[0].LocalID)
	}
}

// TestParseLLMResponse_FencedJSON confirms the JSON extractor strips
// markdown fences before unmarshalling.
func TestParseLLMResponse_FencedJSON(t *testing.T) {
	resp := "```json\n" + `{"plan":{"title":"X"},"tasks":[{"local_id":"T1","title":"A","slots":[]}]}` + "\n```"
	res, warn := parseLLMResponse(resp, "fallback", nil)
	if len(warn) != 0 {
		t.Fatalf("unexpected warnings: %v", warn)
	}
	if res.Plan.Title != "X" || len(res.Tasks) != 1 {
		t.Fatalf("fenced JSON not parsed: %+v", res)
	}
}

// TestParseLLMResponse_AutoLocalIDStepOrder fills in missing local_id /
// step_order so downstream materializer always has stable refs.
func TestParseLLMResponse_AutoLocalIDStepOrder(t *testing.T) {
	resp := `{
        "plan": {"title": "X"},
        "tasks": [
          {"title": "first", "slots": []},
          {"title": "second", "slots": []}
        ]
      }`
	res, _ := parseLLMResponse(resp, "fallback", nil)
	if len(res.Tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(res.Tasks))
	}
	if res.Tasks[0].LocalID != "T1" || res.Tasks[1].LocalID != "T2" {
		t.Errorf("auto local_ids = %v", []string{res.Tasks[0].LocalID, res.Tasks[1].LocalID})
	}
	if res.Tasks[0].StepOrder != 1 || res.Tasks[1].StepOrder != 2 {
		t.Errorf("auto step_orders = %v", []int{res.Tasks[0].StepOrder, res.Tasks[1].StepOrder})
	}
}

// TestValidateDAG_NoCycle confirms acyclic graphs produce no warning.
func TestValidateDAG_NoCycle(t *testing.T) {
	tasks := []TaskDraft{
		{LocalID: "T1"},
		{LocalID: "T2", DependsOnLocal: []string{"T1"}},
		{LocalID: "T3", DependsOnLocal: []string{"T1", "T2"}},
	}
	if got := validateDAG(tasks); len(got) != 0 {
		t.Errorf("want no DAG warnings, got %v", got)
	}
}

// TestValidateDAG_Cycle reports the cycle path with DAG_CYCLE prefix.
func TestValidateDAG_Cycle(t *testing.T) {
	tasks := []TaskDraft{
		{LocalID: "T1", DependsOnLocal: []string{"T3"}},
		{LocalID: "T2", DependsOnLocal: []string{"T1"}},
		{LocalID: "T3", DependsOnLocal: []string{"T2"}},
	}
	got := validateDAG(tasks)
	if len(got) != 1 {
		t.Fatalf("want 1 DAG warning, got %v", got)
	}
	if !strings.HasPrefix(got[0], WarnDAGCycle+":") {
		t.Errorf("want %s prefix, got %q", WarnDAGCycle, got[0])
	}
	// Cycle should mention all three nodes (T1->T3->T2->T1 or rotation).
	for _, want := range []string{"T1", "T2", "T3", "->"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("cycle %q missing %q", got[0], want)
		}
	}
}

// TestValidateDAG_SelfLoop catches self-references — the simplest cycle
// shape — and emits DAG_CYCLE.
func TestValidateDAG_SelfLoop(t *testing.T) {
	tasks := []TaskDraft{
		{LocalID: "T1", DependsOnLocal: []string{"T1"}},
	}
	got := validateDAG(tasks)
	if len(got) != 1 || !strings.HasPrefix(got[0], WarnDAGCycle+":") {
		t.Errorf("self-loop warning = %v", got)
	}
}

// TestValidateSlotTaskRefs_Missing flags slots whose TaskLocalID
// doesn't appear in any TaskDraft.
func TestValidateSlotTaskRefs_Missing(t *testing.T) {
	tasks := []TaskDraft{{LocalID: "T1"}}
	slots := []SlotDraft{
		{TaskLocalID: "T1", SlotType: SlotTypeAgentExecution},
		{TaskLocalID: "TX", SlotType: SlotTypeAgentExecution},
		{TaskLocalID: "TY", SlotType: SlotTypeHumanReview},
	}
	got := validateSlotTaskRefs(tasks, slots)
	if len(got) != 1 {
		t.Fatalf("want 1 warning, got %v", got)
	}
	if !strings.HasPrefix(got[0], WarnSlotMissingTask+":") {
		t.Errorf("want %s prefix, got %q", WarnSlotMissingTask, got[0])
	}
	if !strings.Contains(got[0], "TX") || !strings.Contains(got[0], "TY") {
		t.Errorf("warning %q missing TX/TY", got[0])
	}
}

// TestValidateCollabModeSlotComposition_Mismatch warns when an
// agent_exec_human_review task lacks a human_review slot.
func TestValidateCollabModeSlotComposition_Mismatch(t *testing.T) {
	tasks := []TaskDraft{
		{LocalID: "T1", CollaborationMode: "agent_exec_human_review"},
	}
	slots := []SlotDraft{
		{TaskLocalID: "T1", SlotType: SlotTypeAgentExecution},
		// no human_review
	}
	got := validateCollabModeSlotComposition(tasks, slots)
	if len(got) != 1 {
		t.Fatalf("want 1 warning, got %v", got)
	}
	if !strings.Contains(got[0], WarnCollabModeMismatch) ||
		!strings.Contains(got[0], "human_review") ||
		!strings.Contains(got[0], "T1") {
		t.Errorf("unexpected warning %q", got[0])
	}
}

// TestValidateCollabModeSlotComposition_Mixed never warns for mixed
// (no required composition).
func TestValidateCollabModeSlotComposition_Mixed(t *testing.T) {
	tasks := []TaskDraft{{LocalID: "T1", CollaborationMode: "mixed"}}
	if got := validateCollabModeSlotComposition(tasks, nil); len(got) != 0 {
		t.Errorf("mixed should not warn, got %v", got)
	}
}

// TestValidateSkillCoverage_Gap flags skills no candidate agent has.
func TestValidateSkillCoverage_Gap(t *testing.T) {
	tasks := []TaskDraft{
		{LocalID: "T1", RequiredSkills: []string{"design", "exotic-skill"}},
	}
	agents := []AgentIdentity{
		{ID: "a1", Skills: []string{"design"}, Capabilities: []string{"design"}},
	}
	got := validateSkillCoverage(tasks, agents)
	if len(got) != 1 {
		t.Fatalf("want 1 warning, got %v", got)
	}
	if !strings.HasPrefix(got[0], WarnSkillCoverageGap+":") {
		t.Errorf("want %s prefix, got %q", WarnSkillCoverageGap, got[0])
	}
	if !strings.Contains(got[0], "exotic-skill") {
		t.Errorf("warning %q should mention exotic-skill", got[0])
	}
	if strings.Contains(got[0], "design") {
		t.Errorf("warning %q should not mention 'design' (covered)", got[0])
	}
}

// TestValidateSkillCoverage_NoAgents — when no agents are supplied we
// can't know what's covered; suppress the warning entirely.
func TestValidateSkillCoverage_NoAgents(t *testing.T) {
	tasks := []TaskDraft{{LocalID: "T1", RequiredSkills: []string{"x"}}}
	if got := validateSkillCoverage(tasks, nil); len(got) != 0 {
		t.Errorf("no agents should suppress warning, got %v", got)
	}
}

// TestFallbackPlan ensures the no-LLM path produces a usable
// agent_execution + human_review pair on a single T1 task.
func TestFallbackPlan(t *testing.T) {
	res := fallbackPlan("write the spec", []AgentIdentity{{ID: "a-1", Name: "Alice"}})
	if len(res.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(res.Tasks))
	}
	if res.Tasks[0].LocalID != "T1" {
		t.Errorf("LocalID = %q", res.Tasks[0].LocalID)
	}
	if res.Tasks[0].PrimaryAssigneeAgentID != "a-1" {
		t.Errorf("PrimaryAssigneeAgentID = %q", res.Tasks[0].PrimaryAssigneeAgentID)
	}
	if res.Tasks[0].CollaborationMode != "agent_exec_human_review" {
		t.Errorf("CollaborationMode = %q", res.Tasks[0].CollaborationMode)
	}
	if len(res.Slots) != 2 {
		t.Fatalf("want 2 slots, got %d", len(res.Slots))
	}
	wantTypes := map[string]bool{SlotTypeAgentExecution: false, SlotTypeHumanReview: false}
	for _, sl := range res.Slots {
		if sl.TaskLocalID != "T1" {
			t.Errorf("slot %s TaskLocalID = %q", sl.SlotType, sl.TaskLocalID)
		}
		if _, ok := wantTypes[sl.SlotType]; ok {
			wantTypes[sl.SlotType] = true
		}
	}
	for st, seen := range wantTypes {
		if !seen {
			t.Errorf("missing fallback slot %s", st)
		}
	}
}

// TestExtractJSONObject_Cases sanity-checks the JSON peel-off on a few
// shapes the LLM tends to emit.
func TestExtractJSONObject_Cases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"prefix junk\n{\"a\":1}\nsuffix", `{"a":1}`},
		{"```\n{\"a\":{\"b\":2}}\n```", `{"a":{"b":2}}`},
	}
	for _, c := range cases {
		if got := extractJSONObject(c.in); got != c.want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAppendUnique_Dedup confirms warnings aren't duplicated when the
// validator path runs over already-warned results (e.g. malformed →
// fallback → re-validated).
func TestAppendUnique_Dedup(t *testing.T) {
	out := appendUnique([]string{"A", "B"}, "B", "C", "A", "D")
	want := []string{"A", "B", "C", "D"}
	if len(out) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", out, want)
	}
	for i := range out {
		if out[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, out[i], want[i])
		}
	}
}

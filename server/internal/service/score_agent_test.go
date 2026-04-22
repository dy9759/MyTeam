package service

import (
	"strings"
	"testing"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

func TestScoreAgentForTask_PanicsWhenConcurrencyLimitIsNonPositive(t *testing.T) {
	agent := db.Agent{
		Tags: []string{"go"},
	}
	runtime := db.AgentRuntime{
		ConcurrencyLimit: 0,
		CurrentLoad:      0,
	}
	task := db.Task{
		RequiredSkills: []string{"go"},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for non-positive concurrency limit")
		}
		if msg, ok := r.(string); ok {
			if !strings.Contains(msg, "concurrency_limit") {
				t.Fatalf("panic message %q does not mention concurrency_limit", msg)
			}
		}
	}()

	_ = scoreAgentForTask(agent, runtime, task, true)
}

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitSlotInput_PersistsSubmissionHistoryAcrossRuns(t *testing.T) {
	env := setupProjectTaskEnv(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`UPDATE task SET status = 'needs_human' WHERE id = $1`,
		env.TaskID,
	); err != nil {
		t.Fatalf("set task needs_human: %v", err)
	}

	var slotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO participant_slot (
			task_id, slot_type, slot_order, participant_id, participant_type,
			trigger, blocking, required, status
		)
		VALUES ($1, 'human_input', 0, $2, 'member', 'before_execution', true, true, 'ready')
		RETURNING id
	`, env.TaskID, testUserID).Scan(&slotID); err != nil {
		t.Fatalf("insert human input slot: %v", err)
	}

	first := httptest.NewRecorder()
	req := newRequest("POST", "/api/slots/"+slotID+"/submit", map[string]any{
		"content": map[string]any{"answer": "first run"},
		"comment": "first comment",
	})
	req = withURLParam(req, "id", slotID)
	testHandler.SubmitSlotInput(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first SubmitSlotInput: want 200, got %d: %s", first.Code, first.Body.String())
	}

	var secondRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_run (plan_id, project_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id
	`, env.PlanID, env.ProjectID).Scan(&secondRunID); err != nil {
		t.Fatalf("create second run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE task
		SET run_id = $2, status = 'needs_human', updated_at = now()
		WHERE id = $1
	`, env.TaskID, secondRunID); err != nil {
		t.Fatalf("move task to second run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE participant_slot
		SET status = 'ready', content = NULL, completed_at = NULL, updated_at = now()
		WHERE id = $1
	`, slotID); err != nil {
		t.Fatalf("reset slot for second run: %v", err)
	}

	second := httptest.NewRecorder()
	req = newRequest("POST", "/api/slots/"+slotID+"/submit", map[string]any{
		"content": map[string]any{"answer": "second run"},
		"comment": "second comment",
	})
	req = withURLParam(req, "id", slotID)
	testHandler.SubmitSlotInput(second, req)
	if second.Code != http.StatusOK {
		t.Fatalf("second SubmitSlotInput: want 200, got %d: %s", second.Code, second.Body.String())
	}

	list := httptest.NewRecorder()
	req = newRequest("GET", "/api/slots/"+slotID+"/submissions", nil)
	req = withURLParam(req, "id", slotID)
	testHandler.ListSlotSubmissions(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("ListSlotSubmissions: want 200, got %d: %s", list.Code, list.Body.String())
	}

	var resp map[string]any
	decodeJSON(t, list.Body, &resp)
	submissions, _ := resp["submissions"].([]any)
	if len(submissions) != 2 {
		t.Fatalf("want 2 submissions, got %d", len(submissions))
	}

	latest, _ := submissions[0].(map[string]any)
	earliest, _ := submissions[1].(map[string]any)
	if latest["run_id"] != secondRunID {
		t.Fatalf("latest run_id: want %s, got %v", secondRunID, latest["run_id"])
	}
	if latest["comment"] != "second comment" {
		t.Fatalf("latest comment: want second comment, got %v", latest["comment"])
	}
	content, _ := latest["content"].(map[string]any)
	if content["answer"] != "second run" {
		t.Fatalf("latest content.answer: want second run, got %v", content["answer"])
	}
	if earliest["run_id"] != env.RunID {
		t.Fatalf("earliest run_id: want %s, got %v", env.RunID, earliest["run_id"])
	}
	if earliest["comment"] != "first comment" {
		t.Fatalf("earliest comment: want first comment, got %v", earliest["comment"])
	}
	content, _ = earliest["content"].(map[string]any)
	if content["answer"] != "first run" {
		t.Fatalf("earliest content.answer: want first run, got %v", content["answer"])
	}
}

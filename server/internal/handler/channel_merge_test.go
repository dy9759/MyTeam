package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mergeTestChannel(t *testing.T, name string) string {
	t.Helper()

	var channelID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO channel (workspace_id, name, description, created_by, created_by_type)
		VALUES ($1, $2, '', $3, 'member')
		RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&channelID); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, channelID)
	})

	return channelID
}

func mergeTestRequest(t *testing.T, sourceChannelID, targetChannelID string, approvals any, founders any) string {
	t.Helper()

	approvalsJSON, err := json.Marshal(approvals)
	if err != nil {
		t.Fatalf("marshal approvals: %v", err)
	}
	foundersJSON, err := json.Marshal(founders)
	if err != nil {
		t.Fatalf("marshal founders: %v", err)
	}

	var mergeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO merge_request (source_channel_id, target_channel_id, workspace_id, initiated_by, status, approvals, required_founders)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
		RETURNING id
	`, sourceChannelID, targetChannelID, testWorkspaceID, testUserID, approvalsJSON, foundersJSON).Scan(&mergeID); err != nil {
		t.Fatalf("create merge_request: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM merge_request WHERE id = $1`, mergeID)
	})

	return mergeID
}

func TestApproveMergeRequest_InvalidRequiredFoundersJSON(t *testing.T) {
	sourceChannelID := mergeTestChannel(t, "merge-invalid-founders-source")
	targetChannelID := mergeTestChannel(t, "merge-invalid-founders-target")
	mergeID := mergeTestRequest(t, sourceChannelID, targetChannelID, []map[string]any{}, "not-an-array")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/merge-requests/"+mergeID+"/approve", nil)
	req = withURLParam(req, "mergeID", mergeID)
	testHandler.ApproveMergeRequest(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("ApproveMergeRequest: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Fatalf("expected error response body, got %+v", resp)
	}
}

func TestApproveMergeRequest_InvalidApprovalsJSON(t *testing.T) {
	sourceChannelID := mergeTestChannel(t, "merge-invalid-approvals-source")
	targetChannelID := mergeTestChannel(t, "merge-invalid-approvals-target")
	mergeID := mergeTestRequest(t, sourceChannelID, targetChannelID, "not-an-array", []string{testUserID})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/merge-requests/"+mergeID+"/approve", nil)
	req = withURLParam(req, "mergeID", mergeID)
	testHandler.ApproveMergeRequest(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("ApproveMergeRequest: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Fatalf("expected error response body, got %+v", resp)
	}
}

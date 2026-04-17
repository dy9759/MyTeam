package errcode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCodesHaveUniqueStrings(t *testing.T) {
	seen := map[string]bool{}
	codes := []Code{
		AuthUnauthorized, AuthForbidden,
		ProjectNotFound, PlanNotApproved, PlanHasActiveRun,
		PlanGenMalformed, PlanGenTimeout,
		DAGCycle, DAGUnknownTask, DAGSelfRef,
		TaskNotSchedulable, SlotNotReady, SlotAlreadySubmitted,
		ExecutionLeaseExpired,
		AgentNotAvailable, RuntimeOffline, RuntimeOverloaded,
		ArtifactInvalid, ReviewAlreadyDecided,
		MCPToolNotAvailable, MCPPermissionDenied,
		QuotaExceeded, QuotaConcurrentLimit,
		ImpersonationNotOwnAgent, ImpersonationExpired,
	}
	for _, c := range codes {
		if seen[c.Code] {
			t.Errorf("duplicate code %s", c.Code)
		}
		seen[c.Code] = true
	}
	if len(codes) != 25 {
		t.Errorf("expected 25 codes, got %d", len(codes))
	}
}

func TestWriteDefaultMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, DAGCycle, "", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error.Code != "DAG_CYCLE" {
		t.Errorf("code %q", resp.Error.Code)
	}
	if resp.Error.Message != DAGCycle.Message {
		t.Errorf("message %q", resp.Error.Message)
	}
	if resp.Error.Retriable != false {
		t.Errorf("retriable true")
	}
}

func TestWriteOverrideMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, QuotaExceeded, "workspace quota limit reached", map[string]int{"used_usd": 105})
	var resp Response
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error.Message != "workspace quota limit reached" {
		t.Errorf("override not applied: %q", resp.Error.Message)
	}
	details, ok := resp.Error.Details.(map[string]any)
	if !ok || details["used_usd"] != float64(105) {
		t.Errorf("details lost: %#v", resp.Error.Details)
	}
}

func TestWriteRetriableFlag(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, RuntimeOffline, "", nil)
	var resp Response
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.Error.Retriable {
		t.Errorf("runtime offline should be retriable")
	}
}

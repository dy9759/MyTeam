// Package errcode defines the standardized error response format
// and canonical error codes per PRD §13.
package errcode

import (
	"encoding/json"
	"net/http"
)

// Code is a canonical error code with its HTTP mapping and retry hint.
type Code struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"-"`
	Retriable  bool   `json:"retriable"`
	Message    string `json:"-"` // default message; callers can override
}

// Response is the JSON shape clients see.
type Response struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retriable bool   `json:"retriable"`
		Details   any    `json:"details,omitempty"`
	} `json:"error"`
}

var (
	// Auth
	AuthUnauthorized = Code{"AUTH_UNAUTHORIZED", http.StatusUnauthorized, false, "unauthenticated"}
	AuthForbidden    = Code{"AUTH_FORBIDDEN", http.StatusForbidden, false, "forbidden"}

	// Project / Plan
	ProjectNotFound  = Code{"PROJECT_NOT_FOUND", http.StatusNotFound, false, "project not found"}
	PlanNotApproved  = Code{"PLAN_NOT_APPROVED", http.StatusConflict, false, "plan not approved"}
	PlanHasActiveRun = Code{"PLAN_HAS_ACTIVE_RUN", http.StatusConflict, false, "plan has an active run"}
	PlanGenMalformed = Code{"PLAN_GEN_MALFORMED", http.StatusInternalServerError, true, "plan generation returned invalid output"}
	PlanGenTimeout   = Code{"PLAN_GEN_TIMEOUT", http.StatusGatewayTimeout, true, "plan generation timed out"}

	// DAG
	DAGCycle       = Code{"DAG_CYCLE", http.StatusBadRequest, false, "task DAG contains a cycle"}
	DAGUnknownTask = Code{"DAG_UNKNOWN_TASK", http.StatusBadRequest, false, "depends_on references an unknown task"}
	DAGSelfRef     = Code{"DAG_SELF_REF", http.StatusBadRequest, false, "task depends on itself"}

	// Task / Slot / Execution
	TaskNotSchedulable    = Code{"TASK_NOT_SCHEDULABLE", http.StatusConflict, false, "task is not schedulable"}
	SlotNotReady          = Code{"SLOT_NOT_READY", http.StatusConflict, false, "slot is not ready"}
	SlotAlreadySubmitted  = Code{"SLOT_ALREADY_SUBMITTED", http.StatusConflict, false, "slot already submitted"}
	ExecutionLeaseExpired = Code{"EXECUTION_LEASE_EXPIRED", http.StatusGone, false, "execution lease expired"}

	// Agent / Runtime
	AgentNotAvailable = Code{"AGENT_NOT_AVAILABLE", http.StatusServiceUnavailable, true, "agent not available"}
	RuntimeOffline    = Code{"RUNTIME_OFFLINE", http.StatusServiceUnavailable, true, "runtime offline"}
	RuntimeOverloaded = Code{"RUNTIME_OVERLOADED", http.StatusTooManyRequests, true, "runtime overloaded"}

	// Artifact / Review
	ArtifactInvalid      = Code{"ARTIFACT_INVALID", http.StatusBadRequest, false, "artifact is invalid"}
	ReviewAlreadyDecided = Code{"REVIEW_ALREADY_DECIDED", http.StatusConflict, false, "review already decided"}

	// MCP
	MCPToolNotAvailable = Code{"MCP_TOOL_NOT_AVAILABLE", http.StatusNotFound, false, "tool not available in this runtime mode"}
	MCPPermissionDenied = Code{"MCP_PERMISSION_DENIED", http.StatusForbidden, false, "tool permission denied"}

	// Quota
	QuotaExceeded        = Code{"QUOTA_EXCEEDED", http.StatusTooManyRequests, false, "monthly quota exceeded"}
	QuotaConcurrentLimit = Code{"QUOTA_CONCURRENT_LIMIT", http.StatusTooManyRequests, true, "concurrency limit reached"}

	// Impersonation
	ImpersonationNotOwnAgent = Code{"IMPERSONATION_NOT_OWN_AGENT", http.StatusForbidden, false, "cannot impersonate another owner's agent"}
	ImpersonationExpired     = Code{"IMPERSONATION_EXPIRED", http.StatusGone, false, "impersonation session expired"}
)

// Write serializes a Code into the standard JSON error envelope and sends it.
// If message is empty, the Code's default message is used.
// details (optional) is attached to the response; it should be JSON-serializable.
func Write(w http.ResponseWriter, code Code, message string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code.HTTPStatus)

	msg := message
	if msg == "" {
		msg = code.Message
	}

	var resp Response
	resp.Error.Code = code.Code
	resp.Error.Message = msg
	resp.Error.Retriable = code.Retriable
	resp.Error.Details = details

	_ = json.NewEncoder(w).Encode(resp)
}

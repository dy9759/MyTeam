package protocol

// Event types for WebSocket communication between server, web clients, and daemon.
const (
	// Issue events
	EventIssueCreated = "issue:created"
	EventIssueUpdated = "issue:updated"
	EventIssueDeleted = "issue:deleted"

	// Comment events
	EventCommentCreated       = "comment:created"
	EventCommentUpdated       = "comment:updated"
	EventCommentDeleted       = "comment:deleted"
	EventReactionAdded          = "reaction:added"
	EventReactionRemoved        = "reaction:removed"
	EventIssueReactionAdded     = "issue_reaction:added"
	EventIssueReactionRemoved   = "issue_reaction:removed"

	// Agent events
	EventAgentStatus   = "agent:status"
	EventAgentCreated  = "agent:created"
	EventAgentArchived = "agent:archived"
	EventAgentRestored = "agent:restored"

	// Task events (server <-> daemon)
	EventTaskDispatch  = "task:dispatch"
	EventTaskProgress  = "task:progress"
	EventTaskCompleted = "task:completed"
	EventTaskFailed    = "task:failed"
	EventTaskMessage   = "task:message"
	EventTaskCancelled = "task:cancelled"

	// Inbox events
	EventInboxNew           = "inbox:new"
	EventInboxRead          = "inbox:read"
	EventInboxArchived      = "inbox:archived"
	EventInboxBatchRead     = "inbox:batch-read"
	EventInboxBatchArchived = "inbox:batch-archived"

	// Workspace events
	EventWorkspaceUpdated = "workspace:updated"
	EventWorkspaceDeleted = "workspace:deleted"

	// Member events
	EventMemberAdded   = "member:added"
	EventMemberUpdated = "member:updated"
	EventMemberRemoved = "member:removed"

	// Subscriber events
	EventSubscriberAdded   = "subscriber:added"
	EventSubscriberRemoved = "subscriber:removed"

	// Activity events
	EventActivityCreated = "activity:created"

	// Skill events
	EventSkillCreated = "skill:created"
	EventSkillUpdated = "skill:updated"
	EventSkillDeleted = "skill:deleted"

	// Daemon events
	EventDaemonHeartbeat = "daemon:heartbeat"
	EventDaemonRegister  = "daemon:register"

	// Workflow events
	EventWorkflowCreated       = "workflow:created"
	EventWorkflowStarted       = "workflow:started"
	EventWorkflowCompleted     = "workflow:completed"
	EventWorkflowStepStarted   = "workflow:step:started"
	EventWorkflowStepCompleted = "workflow:step:completed"
	EventWorkflowStepFailed    = "workflow:step:failed"

	// Project events
	EventProjectCreated       = "project:created"
	EventProjectUpdated       = "project:updated"
	EventProjectDeleted       = "project:deleted"
	EventProjectStatusChanged = "project:status_changed"

	// Run events
	EventRunStarted   = "run:started"
	EventRunCompleted = "run:completed"
	EventRunFailed    = "run:failed"

	// Plan events
	EventPlanGenerated = "plan:generated"
	EventPlanApproved  = "plan:approved"
	EventPlanRejected  = "plan:rejected"

	// Channel events
	EventChannelUpdated = "channel:updated"

	// Thread events
	EventThreadCreated = "thread:created"
)

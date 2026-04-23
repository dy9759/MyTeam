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
	EventReactionAdded        = "reaction:added"
	EventReactionRemoved      = "reaction:removed"
	EventIssueReactionAdded   = "issue_reaction:added"
	EventIssueReactionRemoved = "issue_reaction:removed"

	// Agent events
	EventAgentStatus   = "agent:status_changed"
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
	EventInboxNew           = "inbox:item_created"
	EventInboxRead          = "inbox:item_read"
	EventInboxArchived      = "inbox:item_resolved"
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

	// Subagent events — subagents are agent rows with kind='subagent';
	// keep their channel separate so UI can badge them distinctly.
	EventSubagentCreated     = "subagent:created"
	EventSubagentUpdated     = "subagent:updated"
	EventSubagentDeleted     = "subagent:deleted"
	EventSubagentSkillLinked = "subagent:skill_linked"
	EventSubagentSkillUnlinked = "subagent:skill_unlinked"

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

	EventProjectBranchCreated  = "project:branch_created"
	EventProjectPRCreated      = "project:pr_created"
	EventProjectPRMerged       = "project:pr_merged"
	EventProjectPRClosed       = "project:pr_closed"
	EventProjectVersionCreated = "project:version_created"
	EventProjectRunStarted     = "project:run_started"
	EventProjectRunCompleted   = "project:run_completed"
	EventProjectRunFailed      = "project:run_failed"
	EventProjectResultCreated  = "project:result_created"

	// Run events
	EventRunStarted   = "run:started"
	EventRunCompleted = "run:completed"
	EventRunFailed    = "run:failed"

	// Plan events
	EventPlanGenerated = "plan:created"
	EventPlanApproved  = "plan:approval_changed"
	EventPlanRejected  = "plan:approval_changed"

	// Channel events
	EventChannelUpdated = "channel:updated"

	// Thread events
	EventThreadCreated = "thread:created"

	// Thread context item events (Plan 3 / Phase 2)
	EventThreadContextItemCreated = "thread_context_item:created"
	EventThreadContextItemDeleted = "thread_context_item:deleted"

	// Message events
	EventMessageCreated = "message:created"
)

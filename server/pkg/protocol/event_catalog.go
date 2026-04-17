package protocol

// EventDomain categorizes events for catalog/documentation purposes.
type EventDomain string

const (
	DomainProject EventDomain = "project"
	DomainAccount EventDomain = "account"
	DomainSession EventDomain = "session" // channel/thread/message
	DomainInbox   EventDomain = "inbox"
)

// EventDef describes a single WebSocket / bus event for documentation + lookup.
type EventDef struct {
	Type        string // matches the string constants in events.go
	Domain      EventDomain
	Description string
	PayloadKeys []string // illustrative — not enforced
}

// EventCatalog enumerates every event the server publishes.
// Keep in sync with PRD §2.2. When you add a new event type in
// events.go, add its entry here with a short description.
var EventCatalog = []EventDef{
	// --- Project domain ---
	{Type: "project:created", Domain: DomainProject, Description: "Project row created.", PayloadKeys: []string{"project_id", "channel_id", "creator_owner_id"}},
	{Type: "project:status_changed", Domain: DomainProject, Description: "project.status transitioned.", PayloadKeys: []string{"project_id", "from", "to"}},
	{Type: "plan:created", Domain: DomainProject, Description: "Plan created.", PayloadKeys: []string{"plan_id", "project_id", "thread_id"}},
	{Type: "plan:approval_changed", Domain: DomainProject, Description: "plan.approval_status changed.", PayloadKeys: []string{"plan_id", "from", "to", "actor_id"}},
	{Type: "run:started", Domain: DomainProject, Description: "ProjectRun started.", PayloadKeys: []string{"run_id", "plan_id"}},
	{Type: "run:completed", Domain: DomainProject, Description: "ProjectRun reached completed terminal state.", PayloadKeys: []string{"run_id", "plan_id"}},
	{Type: "run:failed", Domain: DomainProject, Description: "ProjectRun failed.", PayloadKeys: []string{"run_id", "plan_id", "reason"}},
	{Type: "run:cancelled", Domain: DomainProject, Description: "ProjectRun cancelled.", PayloadKeys: []string{"run_id", "plan_id", "reason"}},
	{Type: "task:status_changed", Domain: DomainProject, Description: "Task state machine transition.", PayloadKeys: []string{"task_id", "run_id", "from", "to"}},
	{Type: "task:agent_assigned", Domain: DomainProject, Description: "Task assignee changed.", PayloadKeys: []string{"task_id", "agent_id"}},
	{Type: "slot:activated", Domain: DomainProject, Description: "Slot waiting→ready.", PayloadKeys: []string{"slot_id", "task_id", "slot_type"}},
	{Type: "slot:submitted", Domain: DomainProject, Description: "Slot submitted output.", PayloadKeys: []string{"slot_id", "task_id"}},
	{Type: "slot:decision", Domain: DomainProject, Description: "Slot approved / revision_requested / rejected.", PayloadKeys: []string{"slot_id", "review_id", "decision"}},
	{Type: "execution:claimed", Domain: DomainProject, Description: "Execution claimed by a runtime.", PayloadKeys: []string{"execution_id", "runtime_id", "agent_id"}},
	{Type: "execution:started", Domain: DomainProject, Description: "Execution began.", PayloadKeys: []string{"execution_id", "context_ref"}},
	{Type: "execution:completed", Domain: DomainProject, Description: "Execution completed.", PayloadKeys: []string{"execution_id", "result"}},
	{Type: "execution:failed", Domain: DomainProject, Description: "Execution failed.", PayloadKeys: []string{"execution_id", "error"}},
	{Type: "execution:progress", Domain: DomainProject, Description: "Streaming progress update.", PayloadKeys: []string{"execution_id", "progress_payload"}},
	{Type: "artifact:created", Domain: DomainProject, Description: "Artifact version created.", PayloadKeys: []string{"artifact_id", "task_id", "version"}},
	{Type: "review:submitted", Domain: DomainProject, Description: "Review decision recorded.", PayloadKeys: []string{"review_id", "artifact_id", "decision"}},

	// --- Account domain ---
	{Type: "agent:created", Domain: DomainAccount, Description: "Agent created.", PayloadKeys: []string{"agent_id", "owner_id", "agent_type"}},
	{Type: "agent:status_changed", Domain: DomainAccount, Description: "agent.status transitioned.", PayloadKeys: []string{"agent_id", "from", "to"}},
	{Type: "agent:identity_card_updated", Domain: DomainAccount, Description: "identity_card fields changed.", PayloadKeys: []string{"agent_id", "updated_fields"}},
	{Type: "runtime:online", Domain: DomainAccount, Description: "Runtime came online.", PayloadKeys: []string{"runtime_id"}},
	{Type: "runtime:offline", Domain: DomainAccount, Description: "Runtime went offline.", PayloadKeys: []string{"runtime_id"}},
	{Type: "runtime:degraded", Domain: DomainAccount, Description: "Runtime degraded.", PayloadKeys: []string{"runtime_id"}},
	{Type: "impersonation:started", Domain: DomainAccount, Description: "Impersonation session created.", PayloadKeys: []string{"session_id", "owner_id", "agent_id"}},
	{Type: "impersonation:ended", Domain: DomainAccount, Description: "Impersonation session ended.", PayloadKeys: []string{"session_id"}},

	// --- Session / Channel / Thread domain ---
	{Type: "channel:created", Domain: DomainSession, Description: "Channel created.", PayloadKeys: []string{"channel_id", "workspace_id"}},
	{Type: "channel:member_added", Domain: DomainSession, Description: "Member joined channel.", PayloadKeys: []string{"channel_id", "member_id"}},
	{Type: "channel:member_removed", Domain: DomainSession, Description: "Member left channel.", PayloadKeys: []string{"channel_id", "member_id"}},
	{Type: "thread:created", Domain: DomainSession, Description: "Thread created.", PayloadKeys: []string{"thread_id", "channel_id", "root_message_id"}},
	{Type: "thread:status_changed", Domain: DomainSession, Description: "thread.status transitioned.", PayloadKeys: []string{"thread_id", "from", "to"}},
	{Type: "message:created", Domain: DomainSession, Description: "Message posted.", PayloadKeys: []string{"message_id", "channel_id", "thread_id", "sender_id"}},
	{Type: "message:updated", Domain: DomainSession, Description: "Message edited.", PayloadKeys: []string{"message_id"}},
	{Type: "message:deleted", Domain: DomainSession, Description: "Message soft-deleted.", PayloadKeys: []string{"message_id"}},
	{Type: "thread_context_item:created", Domain: DomainSession, Description: "Thread context item added.", PayloadKeys: []string{"item_id", "thread_id"}},
	{Type: "thread_context_item:deleted", Domain: DomainSession, Description: "Thread context item removed.", PayloadKeys: []string{"item_id", "thread_id"}},

	// --- Inbox domain ---
	{Type: "inbox:item_created", Domain: DomainInbox, Description: "New inbox item.", PayloadKeys: []string{"item_id", "recipient_id", "type"}},
	{Type: "inbox:item_read", Domain: DomainInbox, Description: "Inbox item marked read.", PayloadKeys: []string{"item_id"}},
	{Type: "inbox:item_resolved", Domain: DomainInbox, Description: "Inbox item resolved.", PayloadKeys: []string{"item_id", "resolution"}},
}

// CatalogByType returns a map keyed by event type for O(1) lookup.
func CatalogByType() map[string]EventDef {
	out := make(map[string]EventDef, len(EventCatalog))
	for _, e := range EventCatalog {
		out[e.Type] = e
	}
	return out
}

// CatalogByDomain returns all events grouped by domain.
func CatalogByDomain() map[EventDomain][]EventDef {
	out := make(map[EventDomain][]EventDef)
	for _, e := range EventCatalog {
		out[e.Domain] = append(out[e.Domain], e)
	}
	return out
}

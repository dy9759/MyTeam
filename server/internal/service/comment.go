// Package service: comment.go centralizes the create-comment side-effects
// (issue-mention expansion, event publish, on_comment trigger, and mentioned-
// agent enqueue) so every entry point that creates a comment — the HTTP
// handler today, the MCP create_comment tool, future ingest paths — fires
// the same behavior. Without this, MCP-created comments silently bypassed the
// agent triggers, breaking @-mention replies.
package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/mention"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CommentService is the single entry point for creating a comment with all
// side effects (mention expansion, event publish, agent enqueue). HTTP and
// MCP both call Create — the only differences they need are HTTP-specific
// concerns (parent comment lookup, attachment linking, response shape) which
// remain in their respective callers.
type CommentService struct {
	Queries *db.Queries
	Bus     *events.Bus
	Tasks   *TaskService
}

// NewCommentService wires the dependencies. Tasks may be nil only in tests
// that explicitly do not exercise enqueue side-effects; production callers
// must supply a non-nil TaskService.
func NewCommentService(q *db.Queries, bus *events.Bus, tasks *TaskService) *CommentService {
	return &CommentService{Queries: q, Bus: bus, Tasks: tasks}
}

// CreateCommentInput is the unified parameter set for Create. Callers map
// their request shape (HTTP body, MCP args) into this.
type CreateCommentInput struct {
	Issue       db.Issue   // verified-in-workspace issue
	AuthorType  string     // "member" or "agent"
	AuthorID    pgtype.UUID // user_id or agent_id
	Content     string     // raw markdown — Create expands MUL-NN identifiers
	CommentType string     // defaults to "comment" when empty
	ParentID    pgtype.UUID // zero-value when not a reply

	// ParentComment is the resolved parent row used for thread-aware trigger
	// gating (member-thread suppression, mention inheritance). Optional.
	ParentComment *db.Comment

	// ExtraEventFields lets HTTP callers attach response-only data to the
	// WebSocket event payload (e.g. attachment list). Merged into the comment
	// payload sub-map under "comment". MCP callers pass nil.
	ExtraEventFields map[string]any
}

// Create runs the full create-comment pipeline: expand mention identifiers,
// insert the row, publish the WS event, fire the on_comment trigger (with
// thread/mention guards), and enqueue tasks for any @mentioned agents.
//
// Returns the inserted comment so callers can attach it to their response.
// Side-effect failures (publish, enqueue) are logged but do NOT fail the
// call — the comment row is the source of truth and a missing trigger is
// recoverable; rolling the row back would be worse for the caller.
func (s *CommentService) Create(ctx context.Context, in CreateCommentInput) (db.Comment, error) {
	// 1. Expand bare issue identifiers (e.g. MUL-117) into mention links.
	//    Mutating in-place is fine — the input is by-value.
	in.Content = mention.ExpandIssueIdentifiers(ctx, s.Queries, in.Issue.WorkspaceID, in.Content)

	commentType := in.CommentType
	if commentType == "" {
		commentType = "comment"
	}

	// 2. Insert the row.
	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     in.Issue.ID,
		WorkspaceID: in.Issue.WorkspaceID,
		AuthorType:  in.AuthorType,
		AuthorID:    in.AuthorID,
		Content:     in.Content,
		Type:        commentType,
		ParentID:    in.ParentID,
	})
	if err != nil {
		return db.Comment{}, err
	}

	// 3. Publish the WS event so connected clients see the new comment.
	s.publishCreated(in.Issue, comment, in.AuthorType, in.AuthorID, in.ExtraEventFields)

	// 4. on_comment trigger: enqueue assignee agent when applicable.
	s.enqueueOnComment(ctx, in.Issue, comment, in.AuthorType, in.ParentComment)

	// 5. @mention triggers: enqueue every mentioned agent (with the same
	//    visibility/dedup rules as the HTTP path).
	s.enqueueMentionedAgents(ctx, in.Issue, comment, in.ParentComment, in.AuthorType, util.UUIDToString(in.AuthorID))

	return comment, nil
}

// publishCreated builds and publishes the EventCommentCreated payload. The
// "comment" sub-map mirrors CommentResponse field-for-field so the frontend
// reducer accepts the event identically regardless of source. ExtraFields
// (e.g. attachments from the HTTP path) are merged into the comment sub-map.
func (s *CommentService) publishCreated(issue db.Issue, c db.Comment, authorType string, authorID pgtype.UUID, extraFields map[string]any) {
	if s.Bus == nil {
		return
	}
	commentMap := map[string]any{
		"id":           util.UUIDToString(c.ID),
		"issue_id":     util.UUIDToString(c.IssueID),
		"author_type":  c.AuthorType,
		"author_id":    util.UUIDToString(c.AuthorID),
		"content":      c.Content,
		"type":         c.Type,
		"parent_id":    util.UUIDToPtr(c.ParentID),
		"created_at":   util.TimestampToString(c.CreatedAt),
		"updated_at":   util.TimestampToString(c.UpdatedAt),
		"reactions":    []any{},
		"attachments":  []any{},
	}
	for k, v := range extraFields {
		commentMap[k] = v
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   authorType,
		ActorID:     util.UUIDToString(authorID),
		Payload: map[string]any{
			"comment":             commentMap,
			"issue_title":         issue.Title,
			"issue_assignee_type": util.TextToPtr(issue.AssigneeType),
			"issue_assignee_id":   util.UUIDToPtr(issue.AssigneeID),
			"issue_status":        issue.Status,
		},
	})
}

// enqueueOnComment fires the assignee-agent on_comment trigger when the
// comment is from a member, the issue is in a non-terminal status, the
// assignee is an agent with a runtime, and the comment doesn't redirect
// elsewhere via @mentions or member-thread replies.
func (s *CommentService) enqueueOnComment(ctx context.Context, issue db.Issue, comment db.Comment, authorType string, parent *db.Comment) {
	if s.Tasks == nil || authorType != "member" {
		return
	}
	if !s.shouldEnqueueOnComment(ctx, issue) {
		return
	}
	if CommentMentionsOthersButNotAssignee(comment.Content, issue) {
		return
	}
	if IsReplyToMemberThread(parent, comment.Content, issue) {
		return
	}
	replyTo := comment.ID
	if comment.ParentID.Valid {
		replyTo = comment.ParentID
	}
	if _, err := s.Tasks.EnqueueTaskForIssue(ctx, issue, replyTo); err != nil {
		slog.Warn("enqueue agent task on comment failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
	}
}

// shouldEnqueueOnComment is a pure read-only check, mirroring the handler's
// version. Lives here so MCP and HTTP share the same gating logic.
func (s *CommentService) shouldEnqueueOnComment(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "done" || issue.Status == "cancelled" {
		return false
	}
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return false
	}
	agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}
	hasPending, err := s.Queries.HasPendingTaskForIssue(ctx, issue.ID)
	if err != nil || hasPending {
		return false
	}
	return true
}

// enqueueMentionedAgents enqueues a task for each @mentioned agent in the
// comment (and inherited from the parent thread root). Skips self-mentions,
// the assignee agent (handled by on_comment), agents without a runtime, and
// private agents mentioned by non-owner non-admin members.
func (s *CommentService) enqueueMentionedAgents(ctx context.Context, issue db.Issue, comment db.Comment, parent *db.Comment, authorType, authorIDStr string) {
	if s.Tasks == nil {
		return
	}
	wsID := util.UUIDToString(issue.WorkspaceID)
	mentions := util.ParseMentions(comment.Content)
	if parent != nil {
		parentMentions := util.ParseMentions(parent.Content)
		seen := make(map[string]bool, len(mentions))
		for _, m := range mentions {
			seen[m.Type+":"+m.ID] = true
		}
		for _, m := range parentMentions {
			if !seen[m.Type+":"+m.ID] {
				mentions = append(mentions, m)
				seen[m.Type+":"+m.ID] = true
			}
		}
	}
	for _, m := range mentions {
		if m.Type != "agent" {
			continue
		}
		if authorType == "agent" && authorIDStr == m.ID {
			continue
		}
		agentUUID := util.ParseUUID(m.ID)
		if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" &&
			issue.AssigneeID.Valid && util.UUIDToString(issue.AssigneeID) == m.ID {
			continue
		}
		agent, err := s.Queries.GetAgent(ctx, agentUUID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			continue
		}
		// Private-agent visibility: only owner or workspace admin/owner may
		// mention. Non-member callers (i.e. agents) bypass this gate.
		if agent.Visibility == "private" && authorType == "member" {
			isOwner := util.UUIDToString(agent.OwnerID) == authorIDStr
			if !isOwner {
				member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
					UserID:      util.ParseUUID(authorIDStr),
					WorkspaceID: util.ParseUUID(wsID),
				})
				if err != nil || (member.Role != "owner" && member.Role != "admin") {
					continue
				}
			}
		}
		hasPending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
			IssueID: issue.ID,
			AgentID: agentUUID,
		})
		if err != nil || hasPending {
			continue
		}
		replyTo := comment.ID
		if comment.ParentID.Valid {
			replyTo = comment.ParentID
		}
		if _, err := s.Tasks.EnqueueTaskForMention(ctx, issue, agentUUID, replyTo); err != nil {
			slog.Warn("enqueue mention agent task failed", "issue_id", util.UUIDToString(issue.ID), "agent_id", m.ID, "error", err)
		}
	}
}

// CommentMentionsOthersButNotAssignee returns true if the comment @mentions
// anyone but does NOT @mention the issue's assignee agent. Pure logic on the
// content string + issue assignee — no DB calls.
func CommentMentionsOthersButNotAssignee(content string, issue db.Issue) bool {
	mentions := util.ParseMentions(content)
	filtered := mentions[:0]
	for _, m := range mentions {
		if m.Type != "issue" {
			filtered = append(filtered, m)
		}
	}
	mentions = filtered
	if len(mentions) == 0 {
		return false
	}
	if util.HasMentionAll(mentions) {
		return true
	}
	if !issue.AssigneeID.Valid {
		return true
	}
	assigneeID := util.UUIDToString(issue.AssigneeID)
	for _, m := range mentions {
		if m.ID == assigneeID {
			return false
		}
	}
	return true
}

// IsReplyToMemberThread returns true if the comment is a reply in a thread
// started by a member and does NOT @mention the issue's assignee agent.
func IsReplyToMemberThread(parent *db.Comment, content string, issue db.Issue) bool {
	if parent == nil {
		return false
	}
	if parent.AuthorType != "member" {
		return false
	}
	if !issue.AssigneeID.Valid {
		return true
	}
	assigneeID := util.UUIDToString(issue.AssigneeID)
	for _, m := range util.ParseMentions(content) {
		if m.ID == assigneeID {
			return false
		}
	}
	for _, m := range util.ParseMentions(parent.Content) {
		if m.ID == assigneeID {
			return false
		}
	}
	return true
}

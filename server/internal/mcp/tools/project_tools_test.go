package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type mcpToolFixture struct {
	ctx         context.Context
	fake        *fakeMCPDB
	q           *db.Queries
	workspaceID uuid.UUID
	userID      uuid.UUID
	agentID     uuid.UUID
}

func newMCPToolFixture(t *testing.T) *mcpToolFixture {
	t.Helper()

	workspaceID := uuid.New()
	userID := uuid.New()
	agentID := uuid.New()

	fake := &fakeMCPDB{
		member: db.Member{
			ID:          testPgUUID(uuid.New()),
			WorkspaceID: testPgUUID(workspaceID),
			UserID:      testPgUUID(userID),
			Role:        "owner",
			CreatedAt:   testTime(1),
		},
		agent: db.Agent{
			ID:                 testPgUUID(agentID),
			WorkspaceID:        testPgUUID(workspaceID),
			Name:               "MCP Tool Test Agent",
			Visibility:         "workspace",
			Status:             "idle",
			MaxConcurrentTasks: 1,
			OwnerID:            testPgUUID(userID),
			CreatedAt:          testTime(1),
			UpdatedAt:          testTime(1),
			RuntimeID:          testPgUUID(uuid.New()),
			AutoReplyEnabled:   pgtype.Bool{Bool: false, Valid: true},
			AgentType:          "personal_agent",
			OwnerType:          "user",
		},
	}

	return &mcpToolFixture{
		ctx:         context.Background(),
		fake:        fake,
		q:           db.New(fake),
		workspaceID: workspaceID,
		userID:      userID,
		agentID:     agentID,
	}
}

func (f *mcpToolFixture) toolContext() mcptool.Context {
	return mcptool.Context{
		WorkspaceID: f.workspaceID,
		UserID:      f.userID,
		AgentID:     f.agentID,
		RuntimeMode: mcptool.RuntimeCloud,
	}
}

func (f *mcpToolFixture) addProject(title, status string) uuid.UUID {
	return f.addProjectInWorkspace(f.workspaceID, title, status)
}

func (f *mcpToolFixture) addProjectInWorkspace(workspaceID uuid.UUID, title, status string) uuid.UUID {
	projectID := uuid.New()
	f.fake.projects = append(f.fake.projects, db.Project{
		ID:                  testPgUUID(projectID),
		WorkspaceID:         testPgUUID(workspaceID),
		Title:               title,
		Description:         pgtype.Text{String: "test project", Valid: true},
		Status:              status,
		CreatedBy:           testPgUUID(f.userID),
		CreatedAt:           testTime(len(f.fake.projects) + 2),
		UpdatedAt:           testTime(len(f.fake.projects) + 2),
		ScheduleType:        "one_time",
		SourceConversations: []byte(`[]`),
		CreatorOwnerID:      testPgUUID(f.userID),
	})
	return projectID
}

func (f *mcpToolFixture) addProjectWithChannel(title string, channelID uuid.UUID) uuid.UUID {
	projectID := f.addProject(title, "running")
	for i := range f.fake.projects {
		if uuidString(f.fake.projects[i].ID) == projectID.String() {
			f.fake.projects[i].ChannelID = testPgUUID(channelID)
			break
		}
	}
	return projectID
}

func (f *mcpToolFixture) addPlan(projectID uuid.UUID) uuid.UUID {
	planID := uuid.New()
	f.fake.plans = append(f.fake.plans, db.Plan{
		ID:             testPgUUID(planID),
		WorkspaceID:    testPgUUID(f.workspaceID),
		Title:          "Plan for " + projectID.String(),
		CreatedBy:      testPgUUID(f.userID),
		CreatedAt:      testTime(len(f.fake.plans) + 10),
		UpdatedAt:      testTime(len(f.fake.plans) + 10),
		ApprovalStatus: "draft",
		ProjectID:      testPgUUID(projectID),
		AssignedAgents: []byte(`[]`),
	})
	return planID
}

func (f *mcpToolFixture) addAssignedTask(planID uuid.UUID) {
	f.fake.tasks = append(f.fake.tasks, db.Task{
		ID:                testPgUUID(uuid.New()),
		PlanID:            testPgUUID(planID),
		WorkspaceID:       testPgUUID(f.workspaceID),
		Title:             "Assigned task",
		StepOrder:         1,
		PrimaryAssigneeID: testPgUUID(f.agentID),
		Status:            "draft",
		DependsOn:         []pgtype.UUID{},
		FallbackAgentIds:  []pgtype.UUID{},
		RequiredSkills:    []string{},
		CreatedAt:         testTime(len(f.fake.tasks) + 20),
		UpdatedAt:         testTime(len(f.fake.tasks) + 20),
	})
}

func (f *mcpToolFixture) addThread(channelID uuid.UUID, title string) uuid.UUID {
	threadID := uuid.New()
	f.fake.threads = append(f.fake.threads, db.Thread{
		ID:             testPgUUID(threadID),
		ChannelID:      testPgUUID(channelID),
		Title:          pgtype.Text{String: title, Valid: true},
		ReplyCount:     0,
		CreatedAt:      testTime(len(f.fake.threads) + 30),
		WorkspaceID:    testPgUUID(f.workspaceID),
		CreatedBy:      testPgUUID(f.userID),
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		Status:         "active",
		Metadata:       []byte(`{}`),
		LastActivityAt: testTime(len(f.fake.threads) + 30),
	})
	return threadID
}

func (f *mcpToolFixture) addContextItem(threadID uuid.UUID, title, body string) {
	f.fake.contextItems = append(f.fake.contextItems, db.ThreadContextItem{
		ID:             testPgUUID(uuid.New()),
		WorkspaceID:    testPgUUID(f.workspaceID),
		ThreadID:       testPgUUID(threadID),
		ItemType:       "summary",
		Title:          pgtype.Text{String: title, Valid: true},
		Body:           pgtype.Text{String: body, Valid: true},
		Metadata:       []byte(`{}`),
		RetentionClass: "permanent",
		CreatedBy:      testPgUUID(f.userID),
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		CreatedAt:      testTime(len(f.fake.contextItems) + 40),
	})
}

func (f *mcpToolFixture) addProjectFile(projectID uuid.UUID, fileName string, accessScope []byte) {
	f.fake.files = append(f.fake.files, db.FileIndex{
		ID:                   testPgUUID(uuid.New()),
		WorkspaceID:          testPgUUID(f.workspaceID),
		UploaderIdentityID:   testPgUUID(f.userID),
		UploaderIdentityType: "member",
		OwnerID:              testPgUUID(f.userID),
		SourceType:           "project",
		SourceID:             testPgUUID(projectID),
		FileName:             fileName,
		FileSize:             pgtype.Int8{Int64: 123, Valid: true},
		ContentType:          pgtype.Text{String: "text/plain", Valid: true},
		StoragePath:          pgtype.Text{String: "/tmp/" + fileName, Valid: true},
		AccessScope:          accessScope,
		ProjectID:            testPgUUID(projectID),
		CreatedAt:            testTime(len(f.fake.files) + 50),
	})
}

func TestListAssignedProjectsReturnsOnlyAssignedWorkspaceProjects(t *testing.T) {
	f := newMCPToolFixture(t)
	assignedProjectID := f.addProject("Assigned running project", "running")
	assignedPlanID := f.addPlan(assignedProjectID)
	f.addAssignedTask(assignedPlanID)

	unassignedProjectID := f.addProject("Unassigned running project", "running")
	f.addPlan(unassignedProjectID)

	otherWorkspaceProjectID := f.addProjectInWorkspace(uuid.New(), "Other workspace project", "running")
	otherWorkspacePlanID := f.addPlan(otherWorkspaceProjectID)
	f.addAssignedTask(otherWorkspacePlanID)

	result, err := (ListAssignedProjects{}).Exec(f.ctx, f.q, f.toolContext(), map[string]any{
		"status": "running",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Stub {
		t.Fatalf("expected real result, got stub: %#v", result)
	}

	var data struct {
		Projects []struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	marshalResultData(t, result.Data, &data)

	if len(data.Projects) != 1 {
		t.Fatalf("expected one assigned project, got %#v", data.Projects)
	}
	if data.Projects[0].ID != assignedProjectID.String() {
		t.Fatalf("expected assigned project %s, got %s", assignedProjectID, data.Projects[0].ID)
	}
}

func TestListAssignedProjectsDeniesHumanCaller(t *testing.T) {
	f := newMCPToolFixture(t)
	f.addProject("Assigned running project", "running")

	result, err := (ListAssignedProjects{}).Exec(f.ctx, f.q, mcptool.Context{
		WorkspaceID: f.workspaceID,
		UserID:      f.userID,
		RuntimeMode: mcptool.RuntimeCloud,
	}, map[string]any{})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if !containsError(result.Errors, errcode.MCPPermissionDenied.Code) {
		t.Fatalf("expected MCP_PERMISSION_DENIED, got %#v", result)
	}
	if result.Note != "agent context required to list assigned projects" {
		t.Fatalf("unexpected note %q", result.Note)
	}
}

func TestGetProjectReturnsProjectFromCallerWorkspace(t *testing.T) {
	f := newMCPToolFixture(t)
	projectID := f.addProject("Readable project", "not_started")
	otherProjectID := f.addProjectInWorkspace(uuid.New(), "Other workspace project", "not_started")

	result, err := (GetProject{}).Exec(f.ctx, f.q, f.toolContext(), map[string]any{
		"project_id": projectID.String(),
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Stub {
		t.Fatalf("expected real result, got stub: %#v", result)
	}

	var data struct {
		Project struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspace_id"`
			Title       string `json:"title"`
		} `json:"project"`
	}
	marshalResultData(t, result.Data, &data)

	if data.Project.ID != projectID.String() {
		t.Fatalf("expected project %s, got %s", projectID, data.Project.ID)
	}
	if data.Project.WorkspaceID != f.workspaceID.String() {
		t.Fatalf("expected workspace %s, got %s", f.workspaceID, data.Project.WorkspaceID)
	}
	if data.Project.Title != "Readable project" {
		t.Fatalf("unexpected title %q", data.Project.Title)
	}

	result, err = (GetProject{}).Exec(f.ctx, f.q, f.toolContext(), map[string]any{
		"project_id": otherProjectID.String(),
	})
	if err != nil {
		t.Fatalf("exec other workspace: %v", err)
	}
	if !containsError(result.Errors, errcode.ProjectNotFound.Code) {
		t.Fatalf("expected project not found error, got %#v", result)
	}
	if result.Note != errcode.ProjectNotFound.Message {
		t.Fatalf("expected note %q, got %q", errcode.ProjectNotFound.Message, result.Note)
	}
}

func TestSearchProjectContextReturnsMatchingItemsForProjectThreads(t *testing.T) {
	f := newMCPToolFixture(t)
	channelID := uuid.New()
	projectID := f.addProjectWithChannel("Context project", channelID)

	threadID := f.addThread(channelID, "Project thread")
	f.addContextItem(threadID, "Decision", "Use pgvector for semantic needle search later")
	f.addContextItem(threadID, "Unrelated", "This should not match")

	otherChannelID := uuid.New()
	otherProjectID := f.addProjectWithChannel("Other context project", otherChannelID)
	_ = otherProjectID
	otherThreadID := f.addThread(otherChannelID, "Other project thread")
	f.addContextItem(otherThreadID, "Decision", "needle should stay outside project")

	result, err := (SearchProjectContext{}).Exec(f.ctx, f.q, f.toolContext(), map[string]any{
		"project_id": projectID.String(),
		"query":      "needle",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Stub {
		t.Fatalf("expected real result, got stub: %#v", result)
	}

	var data struct {
		Items []struct {
			ThreadID string `json:"thread_id"`
			Body     string `json:"body"`
		} `json:"items"`
	}
	marshalResultData(t, result.Data, &data)

	if len(data.Items) != 1 {
		t.Fatalf("expected one matching context item, got %#v", data.Items)
	}
	if data.Items[0].ThreadID != threadID.String() {
		t.Fatalf("expected thread %s, got %s", threadID, data.Items[0].ThreadID)
	}
	if !strings.Contains(data.Items[0].Body, "needle") {
		t.Fatalf("expected matching body, got %q", data.Items[0].Body)
	}
}

func TestListProjectFilesReturnsProjectScopedFilesOnly(t *testing.T) {
	f := newMCPToolFixture(t)
	projectID := f.addProject("File project", "running")
	otherProjectID := f.addProject("Other file project", "running")

	f.addProjectFile(projectID, "src/main.go", []byte(`{"scope":"project"}`))
	f.addProjectFile(projectID, "src/channel-only.go", []byte(`{"scope":"channel"}`))
	f.addProjectFile(otherProjectID, "src/main.go", []byte(`{"scope":"project"}`))

	result, err := (ListProjectFiles{}).Exec(f.ctx, f.q, f.toolContext(), map[string]any{
		"project_id":  projectID.String(),
		"path_prefix": "src/",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Stub {
		t.Fatalf("expected real result, got stub: %#v", result)
	}

	var data struct {
		Files []struct {
			ProjectID string `json:"project_id"`
			FileName  string `json:"file_name"`
		} `json:"files"`
	}
	marshalResultData(t, result.Data, &data)

	if len(data.Files) != 1 {
		t.Fatalf("expected one project scoped file, got %#v", data.Files)
	}
	if data.Files[0].ProjectID != projectID.String() {
		t.Fatalf("expected project %s, got %s", projectID, data.Files[0].ProjectID)
	}
	if data.Files[0].FileName != "src/main.go" {
		t.Fatalf("expected src/main.go, got %s", data.Files[0].FileName)
	}
}

func marshalResultData(t *testing.T, data any, out any) {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal result data: %v", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal result data %s: %v", string(b), err)
	}
}

func containsError(errors []string, want string) bool {
	for _, got := range errors {
		if got == want {
			return true
		}
	}
	return false
}

type fakeMCPDB struct {
	member       db.Member
	agent        db.Agent
	projects     []db.Project
	plans        []db.Plan
	tasks        []db.Task
	threads      []db.Thread
	contextItems []db.ThreadContextItem
	files        []db.FileIndex
}

func (f *fakeMCPDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeMCPDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "FROM member") && strings.Contains(sql, "WHERE user_id = $1") && strings.Contains(sql, "workspace_id = $2"):
		if len(args) == 2 && pgUUIDEqual(args[0], f.member.UserID) && pgUUIDEqual(args[1], f.member.WorkspaceID) {
			return fakeRow(memberValues(f.member))
		}
	case strings.Contains(sql, "FROM agent") && strings.Contains(sql, "WHERE id = $1") && strings.Contains(sql, "workspace_id = $2"):
		if len(args) == 2 && pgUUIDEqual(args[0], f.agent.ID) && pgUUIDEqual(args[1], f.agent.WorkspaceID) {
			return fakeRow(agentValues(f.agent))
		}
	case strings.Contains(sql, "FROM project") && strings.Contains(sql, "WHERE id = $1"):
		for _, project := range f.projects {
			if len(args) == 1 && pgUUIDEqual(args[0], project.ID) {
				return fakeRow(projectValues(project))
			}
		}
	case strings.Contains(sql, "FROM plan") && strings.Contains(sql, "WHERE project_id = $1"):
		for _, plan := range f.plans {
			if len(args) == 1 && pgUUIDEqual(args[0], plan.ProjectID) {
				return fakeRow(planValues(plan))
			}
		}
	}
	return fakeRowError(pgx.ErrNoRows)
}

func (f *fakeMCPDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM project") && strings.Contains(sql, "WHERE workspace_id = $1"):
		rows := [][]any{}
		for _, project := range f.projects {
			if len(args) == 1 && pgUUIDEqual(args[0], project.WorkspaceID) {
				rows = append(rows, projectValues(project))
			}
		}
		return &fakeRows{rows: rows}, nil
	case strings.Contains(sql, "FROM task WHERE plan_id = $1"):
		rows := [][]any{}
		for _, task := range f.tasks {
			if len(args) == 1 && pgUUIDEqual(args[0], task.PlanID) {
				rows = append(rows, taskValues(task))
			}
		}
		return &fakeRows{rows: rows}, nil
	case strings.Contains(sql, "FROM thread") && strings.Contains(sql, "WHERE channel_id = $1"):
		rows := [][]any{}
		for _, thread := range f.threads {
			if len(args) >= 1 && pgUUIDEqual(args[0], thread.ChannelID) {
				rows = append(rows, threadValues(thread))
			}
		}
		return &fakeRows{rows: rows}, nil
	case strings.Contains(sql, "FROM thread_context_item") && strings.Contains(sql, "WHERE thread_id = $1"):
		rows := [][]any{}
		for _, item := range f.contextItems {
			if len(args) >= 1 && pgUUIDEqual(args[0], item.ThreadID) {
				rows = append(rows, contextItemValues(item))
			}
		}
		return &fakeRows{rows: rows}, nil
	case strings.Contains(sql, "FROM file_index") && strings.Contains(sql, "WHERE project_id = $1"):
		rows := [][]any{}
		for _, file := range f.files {
			if len(args) == 1 && pgUUIDEqual(args[0], file.ProjectID) {
				rows = append(rows, fileIndexValues(file))
			}
		}
		return &fakeRows{rows: rows}, nil
	}
	return &fakeRows{err: fmt.Errorf("unexpected query: %s", sql)}, nil
}

type fakeRowImpl struct {
	values []any
	err    error
}

func fakeRow(values []any) pgx.Row {
	return fakeRowImpl{values: values}
}

func fakeRowError(err error) pgx.Row {
	return fakeRowImpl{err: err}
}

func (r fakeRowImpl) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanValues(r.values, dest...)
}

type fakeRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRows) Next() bool {
	if r.err != nil || r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return fmt.Errorf("scan called before Next")
	}
	return scanValues(r.rows[r.idx-1], dest...)
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, fmt.Errorf("values called before Next")
	}
	return r.rows[r.idx-1], nil
}

func scanValues(values []any, dest ...any) error {
	if len(dest) != len(values) {
		return fmt.Errorf("scan destination count %d does not match values %d", len(dest), len(values))
	}
	for i := range dest {
		dst := reflect.ValueOf(dest[i])
		if dst.Kind() != reflect.Ptr || dst.IsNil() {
			return fmt.Errorf("destination %d is not a non-nil pointer", i)
		}
		elem := dst.Elem()
		if values[i] == nil {
			elem.Set(reflect.Zero(elem.Type()))
			continue
		}
		src := reflect.ValueOf(values[i])
		if !src.Type().AssignableTo(elem.Type()) {
			return fmt.Errorf("cannot assign %s to %s at %d", src.Type(), elem.Type(), i)
		}
		elem.Set(src)
	}
	return nil
}

func memberValues(m db.Member) []any {
	return []any{m.ID, m.WorkspaceID, m.UserID, m.Role, m.CreatedAt}
}

func agentValues(a db.Agent) []any {
	return []any{
		a.ID, a.WorkspaceID, a.Name, a.AvatarUrl, a.Visibility, a.Status,
		a.MaxConcurrentTasks, a.OwnerID, a.CreatedAt, a.UpdatedAt, a.Description,
		a.RuntimeID, a.Instructions, a.ArchivedAt, a.ArchivedBy,
		a.AutoReplyEnabled, a.AutoReplyConfig, a.DisplayName, a.Avatar, a.Bio, a.Tags,
		a.TriggerOnChannelMention, a.NeedsAttention, a.NeedsAttentionReason,
		a.AgentType, a.IdentityCard, a.LastActiveAt, a.Scope, a.OwnerType,
		a.Kind, a.IsGlobal, a.Source, a.SourceRef, a.Category,
	}
}

func projectValues(p db.Project) []any {
	return []any{
		p.ID, p.WorkspaceID, p.Title, p.Description, p.Status, p.CreatedBy,
		p.PlanID, p.CreatedAt, p.UpdatedAt, p.ScheduleType, p.CronExpr,
		p.SourceConversations, p.ChannelID, p.CreatorOwnerID,
	}
}

func planValues(p db.Plan) []any {
	return []any{
		p.ID, p.WorkspaceID, p.Title, p.Description, p.SourceType, p.SourceRefID,
		p.Constraints, p.ExpectedOutput, p.CreatedBy, p.CreatedAt, p.UpdatedAt,
		p.ApprovalStatus, p.ApprovedBy, p.ApprovedAt, p.ProjectID, p.VersionID,
		p.TaskBrief, p.AssignedAgents, p.RiskPoints, p.ThreadID,
		p.InputFiles, p.UserInputs,
	}
}

func taskValues(t db.Task) []any {
	return []any{
		t.ID, t.PlanID, t.RunID, t.WorkspaceID, t.Title, t.Description,
		t.StepOrder, t.DependsOn, t.PrimaryAssigneeID, t.FallbackAgentIds,
		t.RequiredSkills, t.CollaborationMode, t.AcceptanceCriteria, t.Status,
		t.ActualAgentID, t.CurrentRetry, t.StartedAt, t.CompletedAt, t.Result,
		t.Error, t.TimeoutRule, t.RetryRule, t.EscalationPolicy, t.InputContextRefs,
		t.OutputRefs, t.CreatedAt, t.UpdatedAt,
	}
}

func threadValues(t db.Thread) []any {
	return []any{
		t.ID, t.ChannelID, t.Title, t.ReplyCount, t.LastReplyAt, t.CreatedAt,
		t.WorkspaceID, t.RootMessageID, t.IssueID, t.CreatedBy, t.CreatedByType,
		t.Status, t.Metadata, t.LastActivityAt,
	}
}

func contextItemValues(i db.ThreadContextItem) []any {
	return []any{
		i.ID, i.WorkspaceID, i.ThreadID, i.ItemType, i.Title, i.Body,
		i.Metadata, i.SourceMessageID, i.RetentionClass, i.ExpiresAt,
		i.CreatedBy, i.CreatedByType, i.CreatedAt,
	}
}

func fileIndexValues(f db.FileIndex) []any {
	return []any{
		f.ID, f.WorkspaceID, f.UploaderIdentityID, f.UploaderIdentityType,
		f.OwnerID, f.SourceType, f.SourceID, f.FileName, f.FileSize,
		f.ContentType, f.StoragePath, f.AccessScope, f.ChannelID, f.ProjectID,
		f.CreatedAt, f.Backend,
	}
}

func testPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgUUIDEqual(got any, want pgtype.UUID) bool {
	gotUUID, ok := got.(pgtype.UUID)
	return ok && gotUUID.Valid == want.Valid && gotUUID.Bytes == want.Bytes
}

func testTime(offset int) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Unix(int64(offset), 0).UTC(), Valid: true}
}

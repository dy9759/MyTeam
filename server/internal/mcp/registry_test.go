package mcp

import "testing"

func TestRegistryHas17Tools(t *testing.T) {
	if len(Registry) != 17 {
		t.Errorf("expected 17 tools, got %d", len(Registry))
	}
}

func TestNoDuplicateToolNames(t *testing.T) {
	names := List()
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate %q", n)
		}
		seen[n] = true
	}
}

func TestGetReturnsRegistered(t *testing.T) {
	cases := []string{
		"get_issue", "list_issue_comments", "create_comment", "update_issue_status",
		"list_assigned_projects", "get_project", "search_project_context", "list_project_files",
		"download_attachment", "upload_artifact", "complete_task", "request_approval",
		"read_file", "apply_patch", "create_pr", "checkout_repo", "local_file_read",
	}
	for _, name := range cases {
		if Get(name) == nil {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestLocalOnlyTools(t *testing.T) {
	for _, name := range []string{"checkout_repo", "local_file_read"} {
		modes := Get(name).RuntimeModes()
		if len(modes) != 1 || modes[0] != "local" {
			t.Errorf("%q should be local-only; got %v", name, modes)
		}
	}
}

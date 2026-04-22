package mcp

import (
	"fmt"
	"sort"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/tools"
)

// Registry holds every Tool keyed by Name(). Built once in init().
var Registry = map[string]Tool{}

func init() {
	for _, t := range []Tool{
		tools.GetIssue{},
		tools.ListIssueComments{},
		tools.CreateComment{},
		tools.UpdateIssueStatus{},
		tools.ListAssignedProjects{},
		tools.GetProject{},
		tools.SearchProjectContext{},
		tools.ListProjectFiles{},
		tools.DownloadAttachment{},
		tools.UploadArtifact{},
		tools.CompleteTask{},
		tools.RequestApproval{},
		tools.ReadFile{},
		tools.ApplyPatch{},
		tools.CreatePR{},
		tools.CheckoutRepo{},
		tools.LocalFileRead{},
		// Memory MCP tools (Phase G).
		tools.MemorySearch{},
		tools.MemoryAppend{},
		tools.MemoryPromote{},
		tools.MemoryList{},
	} {
		if _, dup := Registry[t.Name()]; dup {
			panic(fmt.Sprintf("mcp: duplicate tool name %q", t.Name()))
		}
		Registry[t.Name()] = t
	}
}

// Get returns a tool by name. Returns nil if not found.
func Get(name string) Tool {
	return Registry[name]
}

// List returns all tool names sorted lexicographically.
func List() []string {
	names := make([]string, 0, len(Registry))
	for name := range Registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

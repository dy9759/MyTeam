package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type SearchResult struct {
	Type    string  `json:"type"` // "message", "issue", "agent", "file"
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Preview string  `json:"preview"`
	Score   float64 `json:"score"`
}

func pgTextToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// GET /api/search?q=keyword&type=message,issue,agent
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' required")
		return
	}

	searchTypes := r.URL.Query().Get("type")
	if searchTypes == "" {
		searchTypes = "message,issue,agent"
	}
	types := strings.Split(searchTypes, ",")

	workspaceID := resolveWorkspaceID(r)
	var results []SearchResult

	for _, t := range types {
		switch strings.TrimSpace(t) {
		case "message":
			channels, _ := h.Queries.ListChannels(r.Context(), parseUUID(workspaceID))
			for _, ch := range channels {
				msgs, _ := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
					ChannelID: ch.ID, Limit: 20, Offset: 0,
				})
				for _, m := range msgs {
					if strings.Contains(strings.ToLower(m.Content), strings.ToLower(query)) {
						results = append(results, SearchResult{
							Type:    "message",
							ID:      uuidToString(m.ID),
							Title:   truncateStr(m.Content, 60),
							Preview: m.Content,
						})
					}
				}
			}
			slog.Debug("searched messages", "q", query, "workspace", workspaceID, "found", len(results))
		case "issue":
			issues, err := h.Queries.ListIssues(r.Context(), db.ListIssuesParams{
				WorkspaceID: parseUUID(workspaceID),
				Limit:       10,
				Offset:      0,
			})
			if err == nil {
				for _, issue := range issues {
					title := issue.Title
					if strings.Contains(strings.ToLower(title), strings.ToLower(query)) {
						results = append(results, SearchResult{
							Type:    "issue",
							ID:      uuidToString(issue.ID),
							Title:   title,
							Preview: pgTextToString(issue.Description),
						})
					}
				}
			}
		case "agent":
			agents, err := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
			if err == nil {
				for _, a := range agents {
					name := a.Name
					if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
						results = append(results, SearchResult{
							Type:    "agent",
							ID:      uuidToString(a.ID),
							Title:   name,
							Preview: a.Description,
						})
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results, "total": len(results)})
}

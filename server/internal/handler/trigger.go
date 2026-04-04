package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ParseMentions extracts @agent mentions from text.
func ParseMentions(text string) []string {
	var mentions []string
	words := strings.Fields(text)
	for _, word := range words {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			name := strings.TrimPrefix(word, "@")
			name = strings.TrimRight(name, ".,;:!?")
			if name != "" {
				mentions = append(mentions, name)
			}
		}
	}
	return mentions
}

// POST /api/triggers/check-mentions
func (h *Handler) CheckMentions(w http.ResponseWriter, r *http.Request) {
	type Request struct {
		Text        string `json:"text"`
		WorkspaceID string `json:"workspace_id"`
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	mentions := ParseMentions(req.Text)

	writeJSON(w, http.StatusOK, map[string]any{
		"mentions":    mentions,
		"has_mention": len(mentions) > 0,
	})
}

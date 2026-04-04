package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

type TriggerHandler struct{}

func NewTriggerHandler() *TriggerHandler {
	return &TriggerHandler{}
}

// ParseMentions extracts @agent mentions from text
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

// POST /api/triggers/check-mentions — Check if message text mentions any agents
func (h *TriggerHandler) CheckMentions(w http.ResponseWriter, r *http.Request) {
	type Request struct {
		Text        string `json:"text"`
		WorkspaceID string `json:"workspace_id"`
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	mentions := ParseMentions(req.Text)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"mentions":    mentions,
		"has_mention": len(mentions) > 0,
	})
}

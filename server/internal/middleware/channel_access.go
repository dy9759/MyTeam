package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// RequireChannelMember checks if the user is a member of the channel
func RequireChannelMember(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			channelID := chi.URLParam(r, "channelID")
			userID := r.Header.Get("X-User-ID")

			if channelID == "" || userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			members, err := queries.ListChannelMembers(r.Context(), util.ParseUUID(channelID))
			if err != nil {
				next.ServeHTTP(w, r) // fail open for now
				return
			}

			isMember := false
			for _, m := range members {
				if util.UUIDToString(m.MemberID) == userID {
					isMember = true
					break
				}
			}

			if !isMember {
				ch, err := queries.GetChannel(r.Context(), util.ParseUUID(channelID))
				if err != nil || ch.Visibility != "public" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]string{"error": "not a channel member"})
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

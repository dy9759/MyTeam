// Browser-side WebSocket relay to Volcengine sauc bigmodel streaming ASR.
//
// Protocol between browser and this handler (all JSON text frames except
// PCM which goes as binary):
//
//	Browser → Server:
//	  1. First text frame: {"type":"config","meeting_id":"…","language":"zh-CN","enable_speaker":true,"hot_words":[…],"topic":"…"}
//	  2. Any number of binary frames: raw 16-bit LE PCM @ 16kHz mono
//	  3. Text frame: {"type":"end"} when user stops
//
//	Server → Browser:
//	  - {"type":"ready","log_id":"…"} once upstream accepts the config
//	  - {"type":"utterances","text":"…","utterances":[{speaker_id,text,...}]}
//	    streamed as upstream emits them
//	  - {"type":"error","message":"…"} on upstream failure
//	  - {"type":"done"} when upstream signals last packet
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/MyAIOSHub/MyTeam/server/internal/auth"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/asr/sauc"
)

// asrStreamUpgrader is the per-handler upgrader. CheckOrigin is
// intentionally permissive because the main /ws endpoint runs with the
// same policy; tighten both together in production.
var asrStreamUpgrader = websocket.Upgrader{
	ReadBufferSize:  1 << 15,
	WriteBufferSize: 1 << 15,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// browserConfigFrame is the first JSON the browser sends.
type browserConfigFrame struct {
	Type          string   `json:"type"`
	MeetingID     string   `json:"meeting_id"`
	Language      string   `json:"language"`
	EnableSpeaker bool     `json:"enable_speaker"`
	HotWords      []string `json:"hot_words"`
	Topic         string   `json:"topic"`
}

// browserControlFrame is a generic text frame the browser uses to signal
// stream termination ({"type":"end"}).
type browserControlFrame struct {
	Type string `json:"type"`
}

// StreamLiveASR handles GET /api/asr/stream — upgrades to WS, spins up
// an upstream sauc session, and bridges traffic both directions.
// Auth: browsers can't set Authorization headers on native WebSocket
// constructors, so the JWT travels as a ?token= query param (same
// pattern as /ws).
func (h *Handler) StreamLiveASR(w http.ResponseWriter, r *http.Request) {
	if _, ok := authenticateWSRequest(w, r); !ok {
		return
	}

	cfg := sauc.LoadConfigFromEnv()
	if !cfg.IsConfigured() {
		writeError(w, http.StatusServiceUnavailable,
			"live ASR unavailable: set MYTEAM_DOUBAO_APP_ID and MYTEAM_DOUBAO_ACCESS_TOKEN")
		return
	}

	conn, err := asrStreamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("asr-stream: upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	// Configure tight read deadlines so a wedged browser connection
	// doesn't keep an upstream session alive.
	conn.SetReadLimit(1 << 20) // 1 MiB per frame is more than enough for PCM
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})

	// 1. Expect a JSON config as the first message.
	mt, raw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	if mt != websocket.TextMessage {
		writeJSONText(conn, map[string]any{
			"type":    "error",
			"message": "first frame must be JSON config",
		})
		return
	}
	var cfgFrame browserConfigFrame
	if err := json.Unmarshal(raw, &cfgFrame); err != nil || cfgFrame.Type != "config" {
		writeJSONText(conn, map[string]any{
			"type":    "error",
			"message": "invalid config frame",
		})
		return
	}

	// 2. Open upstream session.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sess, err := sauc.Dial(ctx, cfg, sauc.RecognitionParams{
		UserID:          cfgFrame.MeetingID,
		Language:        cfgFrame.Language,
		EnableSpeaker:   cfgFrame.EnableSpeaker,
		EnableNonStream: cfgFrame.EnableSpeaker, // diarization requires二遍 on async endpoint
		ShowUtterances:  true,
		HotWords:        cfgFrame.HotWords,
		Topic:           cfgFrame.Topic,
	})
	if err != nil {
		slog.Warn("asr-stream: dial upstream failed", "err", err)
		writeJSONText(conn, map[string]any{
			"type":    "error",
			"message": err.Error(),
		})
		return
	}
	defer sess.Close()

	writeJSONText(conn, map[string]any{
		"type":   "ready",
		"log_id": sess.LogID(),
	})

	// 3. Fan out: reader goroutine pumps upstream → browser; the main
	// loop pumps browser → upstream so we can cleanly tear down when
	// the client closes.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for ev := range sess.Results() {
			if ev.Err != nil {
				writeJSONText(conn, map[string]any{
					"type":    "error",
					"message": ev.Err.Error(),
					"log_id":  ev.LogID,
				})
				continue
			}
			writeJSONText(conn, map[string]any{
				"type":       "utterances",
				"text":       ev.Text,
				"utterances": ev.Utterances,
				"final":      ev.Final,
				"log_id":     ev.LogID,
			})
			if ev.Final {
				writeJSONText(conn, map[string]any{"type": "done"})
				return
			}
		}
	}()

	// 4. Browser → upstream loop.
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		switch mt {
		case websocket.BinaryMessage:
			if sErr := sess.SendAudio(payload, false); sErr != nil {
				slog.Warn("asr-stream: upstream audio write failed", "err", sErr)
				writeJSONText(conn, map[string]any{
					"type":    "error",
					"message": fmt.Sprintf("upstream write: %v", sErr),
				})
				return
			}
		case websocket.TextMessage:
			var ctl browserControlFrame
			if err := json.Unmarshal(payload, &ctl); err != nil {
				continue
			}
			if ctl.Type == "end" {
				// Send empty last audio packet per protocol.
				_ = sess.SendAudio(nil, true)
				// Wait briefly for upstream final response, then exit.
				select {
				case <-readerDone:
				case <-time.After(30 * time.Second):
				}
				return
			}
		}
	}
	// Browser disconnected without clean "end" — cancel upstream.
	cancel()
}

// authenticateWSRequest validates the JWT token query param the same
// way realtime.HandleWebSocket does. Returns the user id on success.
// On failure it writes the HTTP error and returns ok=false.
func authenticateWSRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusUnauthorized)
		return "", false
	}
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return "", false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
		return "", false
	}
	uid, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(uid) == "" {
		http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
		return "", false
	}
	return uid, true
}

// writeJSONText serializes v and sends as a text frame. Write errors are
// swallowed because the connection is being torn down by the caller.
func writeJSONText(conn *websocket.Conn, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteMessage(websocket.TextMessage, raw)
}

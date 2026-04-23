// Upstream client for Volcengine sauc bigmodel_async streaming ASR.
//
// Scope: connect, send first full_client_request, stream audio chunks,
// receive server frames, expose utterances as Go events. Auth headers
// are loaded from env; caller owns the meeting-side lifecycle.
package sauc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// DefaultEndpoint is the recommended bidirectional optimized streaming
// endpoint per the Volcengine docs.
const DefaultEndpoint = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"

// Config holds auth + tuning knobs. Load via LoadConfigFromEnv.
type Config struct {
	Endpoint    string // wss URL, defaults to DefaultEndpoint
	AppID       string // X-Api-App-Key
	AccessToken string // X-Api-Access-Key
	// ResourceIDs is the ordered list of X-Api-Resource-Id values to try.
	// First success wins. Lets operators default to ASR 2.0 (seedasr,
	// better speaker diarization) and transparently fall back to ASR
	// 1.0 (bigasr) when the account only has 1.0 enabled.
	ResourceIDs []string
	// WriteTimeout bounds upstream writes (handshake + audio frames).
	WriteTimeout time.Duration
	// DialTimeout bounds a single WebSocket handshake attempt.
	DialTimeout time.Duration
}

// defaultResourceIDs orders ASR 1.0 (bigasr) as primary — confirmed
// working on the current account — with ASR 2.0 (seedasr) as fallback
// in case 1.0 is ever temporarily unavailable. Concurrent-billing
// variants are included last so an account that only holds concurrent
// packages still connects.
var defaultResourceIDs = []string{
	"volc.bigasr.sauc.duration",
	"volc.bigasr.sauc.concurrent",
	"volc.seedasr.sauc.duration",
	"volc.seedasr.sauc.concurrent",
}

// LoadConfigFromEnv reads the same credentials the submit/query path
// uses (MYTEAM_DOUBAO_*) so operators only configure one set of keys.
// Resource override:
//   - MYTEAM_VOLC_SAUC_RESOURCE_ID (single) forces just that resource.
//   - MYTEAM_VOLC_SAUC_RESOURCE_IDS (comma-separated) defines the full
//     fallback chain in order.
func LoadConfigFromEnv() Config {
	cfg := Config{
		Endpoint:     os.Getenv("MYTEAM_VOLC_SAUC_ENDPOINT"),
		AppID:        os.Getenv("MYTEAM_DOUBAO_APP_ID"),
		AccessToken:  os.Getenv("MYTEAM_DOUBAO_ACCESS_TOKEN"),
		WriteTimeout: 15 * time.Second,
		DialTimeout:  10 * time.Second,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if list := os.Getenv("MYTEAM_VOLC_SAUC_RESOURCE_IDS"); list != "" {
		for _, id := range splitAndTrim(list) {
			if id != "" {
				cfg.ResourceIDs = append(cfg.ResourceIDs, id)
			}
		}
	}
	if single := os.Getenv("MYTEAM_VOLC_SAUC_RESOURCE_ID"); single != "" {
		// Single value acts as the first/only entry, preserving any
		// extra chain entries that came from the list var.
		cfg.ResourceIDs = append([]string{single}, cfg.ResourceIDs...)
	}
	if len(cfg.ResourceIDs) == 0 {
		cfg.ResourceIDs = append(cfg.ResourceIDs, defaultResourceIDs...)
	}
	return cfg
}

// splitAndTrim splits on commas and strips surrounding whitespace.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// IsConfigured reports whether auth credentials are present. Callers
// should 503 the endpoint when false.
func (c Config) IsConfigured() bool {
	return c.AppID != "" && c.AccessToken != "" && len(c.ResourceIDs) > 0
}

// RecognitionParams feeds into the first full_client_request payload.
// Zero values pick sensible defaults: pcm/16k/16bit/mono, speaker info +
// dual-pass recognition turned on (required for real-time diarization
// in the bigmodel_async path).
type RecognitionParams struct {
	UserID          string
	Language        string // e.g. "zh-CN" (empty = auto Chinese+English+dialects)
	EnableSpeaker   bool
	EnableNonStream bool
	ShowUtterances  bool
	HotWords        []string
	Topic           string // surfaced as user.app_version for log tagging
}

// Session is a one-shot upstream connection to Volcengine. Not safe for
// concurrent use beyond the public SendAudio / Close / Results API.
type Session struct {
	cfg    Config
	conn   *websocket.Conn
	logID  string // X-Tt-Logid returned by server on handshake
	connID string // X-Api-Connect-Id we sent

	seq atomic.Int32

	// results is the event channel consumed by the browser relay. Bounded
	// so a slow reader doesn't let upstream inflate memory forever.
	results chan Event
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// Event is what the upstream reader loop hands the relay. Exactly one of
// Utterances / Err will be populated per event (plus Final on terminal).
type Event struct {
	Final      bool
	Utterances []Utterance
	Text       string // full-audio running text when server emits it
	Err        error
	LogID      string
}

// Utterance mirrors the server JSON shape, with speaker_id only present
// when enable_speaker_info was requested and the server populated it.
type Utterance struct {
	Text      string  `json:"text"`
	StartTime int64   `json:"start_time"`
	EndTime   int64   `json:"end_time"`
	Definite  bool    `json:"definite"`
	SpeakerID *int    `json:"speaker_id,omitempty"`
	Language  string  `json:"language,omitempty"`
	Words     []Word  `json:"words,omitempty"`
	Additions Additions `json:"additions,omitempty"`
}

// Word is a single-character segment with offsets. Some fields are only
// populated when show_utterances=true.
type Word struct {
	Text      string `json:"text"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

// Additions carries optional per-utterance metadata (speaker / emotion /
// gender / volume). Fields stay optional so the decoder never errors on
// a missing one.
type Additions struct {
	LogID     string  `json:"log_id,omitempty"`
	Emotion   string  `json:"emotion,omitempty"`
	Gender    string  `json:"gender,omitempty"`
	Volume    float64 `json:"volume,omitempty"`
	LidLang   string  `json:"lid_lang,omitempty"`
	SpeechRate float64 `json:"speech_rate,omitempty"`
}

// serverPayload mirrors the JSON served inside FULL_SERVER_RESPONSE.
type serverPayload struct {
	Code     int    `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	AudioInfo struct {
		Duration int `json:"duration"`
	} `json:"audio_info,omitempty"`
	Result struct {
		Text       string      `json:"text"`
		Utterances []Utterance `json:"utterances"`
	} `json:"result"`
}

// Dial opens the upstream WS and sends the full_client_request. Walks
// the configured ResourceIDs in order; first successful handshake +
// config send wins. Returns a composite error listing every failed
// resource when nothing worked.
func Dial(ctx context.Context, cfg Config, params RecognitionParams) (*Session, error) {
	if !cfg.IsConfigured() {
		return nil, errors.New("sauc: credentials missing (MYTEAM_DOUBAO_APP_ID / _ACCESS_TOKEN / resource ids)")
	}
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("sauc: bad endpoint: %w", err)
	}

	var errs []string
	for _, resourceID := range cfg.ResourceIDs {
		sess, err := dialOne(ctx, cfg, resourceID, params)
		if err == nil {
			slog.Info("sauc: upstream connected",
				"resource_id", resourceID,
				"log_id", sess.logID,
				"conn_id", sess.connID)
			return sess, nil
		}
		slog.Warn("sauc: dial attempt failed, trying next",
			"resource_id", resourceID,
			"err", err)
		errs = append(errs, fmt.Sprintf("%s: %v", resourceID, err))
	}
	return nil, fmt.Errorf("sauc: all resource ids failed: %s", strings.Join(errs, " | "))
}

// dialOne is a single-attempt handshake + config send for one resource.
func dialOne(ctx context.Context, cfg Config, resourceID string, params RecognitionParams) (*Session, error) {
	connID := uuid.New().String()
	header := http.Header{}
	header.Set("X-Api-App-Key", cfg.AppID)
	header.Set("X-Api-Access-Key", cfg.AccessToken)
	header.Set("X-Api-Resource-Id", resourceID)
	header.Set("X-Api-Connect-Id", connID)

	dialer := &websocket.Dialer{
		HandshakeTimeout: cfg.DialTimeout,
	}
	dialCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	conn, resp, err := dialer.DialContext(dialCtx, cfg.Endpoint, header)
	if err != nil {
		status := ""
		logID := ""
		if resp != nil {
			status = resp.Status
			logID = resp.Header.Get("X-Tt-Logid")
		}
		return nil, fmt.Errorf("dial %s (%s logid=%q): %w", cfg.Endpoint, status, logID, err)
	}

	s := &Session{
		cfg:     cfg,
		conn:    conn,
		connID:  connID,
		results: make(chan Event, 32),
		done:    make(chan struct{}),
	}
	if resp != nil {
		s.logID = resp.Header.Get("X-Tt-Logid")
	}
	s.seq.Store(0)

	if err := s.sendFullClientRequest(params); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send config: %w", err)
	}

	go s.readLoop()
	return s, nil
}

// LogID returns the server-assigned tracing id; empty until handshake.
func (s *Session) LogID() string { return s.logID }

// Results is the read-only event stream. Closed after Close() or upstream
// terminates.
func (s *Session) Results() <-chan Event { return s.results }

// SendAudio pushes a PCM chunk upstream. Last=true marks the final
// packet and flips sequence to negative per protocol.
func (s *Session) SendAudio(pcm []byte, last bool) error {
	seq := s.seq.Add(1)
	frame, err := EncodeAudioFrame(pcm, seq, last)
	if err != nil {
		return err
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	return s.conn.WriteMessage(websocket.BinaryMessage, frame)
}

// Close shuts the upstream down. Safe to call multiple times.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		// Best-effort graceful close.
		_ = s.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		s.closeErr = s.conn.Close()
		close(s.done)
	})
	return s.closeErr
}

// sendFullClientRequest serializes the first JSON config frame.
func (s *Session) sendFullClientRequest(p RecognitionParams) error {
	payload := map[string]any{
		"user": map[string]any{
			"uid":         nonEmpty(p.UserID, "myteam-user"),
			"app_version": nonEmpty(p.Topic, ""),
		},
		"audio": map[string]any{
			"format":  "pcm",
			"codec":   "raw",
			"rate":    16000,
			"bits":    16,
			"channel": 1,
		},
		"request": map[string]any{
			"model_name":       "bigmodel",
			"enable_itn":       true,
			"enable_punc":      true,
			"enable_ddc":       false,
			"show_utterances":  p.ShowUtterances || true,
			"end_window_size":  800,
			"enable_nonstream": p.EnableNonStream || p.EnableSpeaker,
		},
	}
	reqMap := payload["request"].(map[string]any)
	if p.Language != "" {
		payload["audio"].(map[string]any)["language"] = p.Language
	}
	if p.EnableSpeaker {
		reqMap["enable_speaker_info"] = true
		reqMap["ssd_version"] = "200"
	}
	if len(p.HotWords) > 0 {
		hotwords := make([]map[string]string, 0, len(p.HotWords))
		for _, w := range p.HotWords {
			hotwords = append(hotwords, map[string]string{"word": w})
		}
		ctxBytes, _ := json.Marshal(map[string]any{"hotwords": hotwords})
		reqMap["corpus"] = map[string]any{"context": string(ctxBytes)}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sauc: marshal config: %w", err)
	}
	seq := s.seq.Add(1)
	frame, err := EncodeFullClientRequest(raw, seq)
	if err != nil {
		return err
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	return s.conn.WriteMessage(websocket.BinaryMessage, frame)
}

// readLoop pumps server frames into the Results channel until the
// connection closes or a terminal frame arrives.
func (s *Session) readLoop() {
	defer close(s.results)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			if !isNormalClose(err) {
				s.push(Event{Err: fmt.Errorf("sauc read: %w", err)})
			}
			return
		}
		frame, err := Decode(raw)
		if err != nil {
			s.push(Event{Err: fmt.Errorf("sauc decode: %w", err)})
			continue
		}
		switch frame.MessageType {
		case MsgServerError:
			msg := string(frame.Payload)
			slog.Warn("sauc: server error",
				"code", frame.ErrorCode,
				"message", msg,
				"logid", s.logID,
				"connid", s.connID)
			s.push(Event{Err: fmt.Errorf("sauc error %d: %s", frame.ErrorCode, msg)})
			return
		case MsgFullServerResponse:
			var p serverPayload
			if err := json.Unmarshal(frame.Payload, &p); err != nil {
				s.push(Event{Err: fmt.Errorf("sauc parse: %w", err)})
				continue
			}
			s.push(Event{
				Final:      frame.IsLast(),
				Utterances: p.Result.Utterances,
				Text:       p.Result.Text,
				LogID:      s.logID,
			})
			if frame.IsLast() {
				return
			}
		default:
			// Unknown type — log, keep reading.
			slog.Debug("sauc: unknown frame type",
				"type", frame.MessageType,
				"flags", frame.Flags,
				"len", len(frame.Payload))
		}
	}
}

// push writes to Results without blocking the reader forever. If the
// consumer stalls for 2s we drop the event rather than wedge the pipe.
func (s *Session) push(e Event) {
	select {
	case s.results <- e:
	case <-time.After(2 * time.Second):
		slog.Warn("sauc: dropped event (slow consumer)", "logid", s.logID)
	case <-s.done:
	}
}

func isNormalClose(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

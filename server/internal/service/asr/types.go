// Package asr defines the ASR + meeting-summary contract used by the
// meeting service. The Doubao 妙记 batch impl lives in this package; the
// streaming WebSocket impl lands in phase 2.
package asr

import (
	"context"
	"errors"
	"time"
)

// Speaker is the diarized speaker tag emitted by the ASR layer. Empty
// when the upstream couldn't identify a speaker (still-valid segment).
type Speaker string

// Segment is a single transcribed utterance with millisecond offsets.
type Segment struct {
	Speaker Speaker       `json:"speaker"`
	Text    string        `json:"text"`
	Start   time.Duration `json:"start"`
	End     time.Duration `json:"end"`
}

// ActionItem is the structured todo extracted from a meeting summary.
// `Owner` and `DueDate` are best-effort — the LLM/妙记 may not pick them
// up. `Confidence` is in [0,1]; the meeting service uses it to decide
// auto-vs-manual task creation thresholds.
type ActionItem struct {
	ID         string  `json:"id"`
	Task       string  `json:"task"`
	Owner      string  `json:"owner,omitempty"`
	DueDate    string  `json:"due_date,omitempty"`
	Confidence float64 `json:"confidence"`
}

// SummaryBundle is what BatchSummarize returns: the prose summary, the
// decisions, the segment-level transcript, and the extracted todos. Any
// field can be empty if 妙记 couldn't infer it.
type SummaryBundle struct {
	Provider    string       `json:"provider"`
	Sections    []string     `json:"sections,omitempty"`
	Decisions   []string     `json:"decisions,omitempty"`
	Segments    []Segment    `json:"segments,omitempty"`
	ActionItems []ActionItem `json:"action_items,omitempty"`
}

// Credentials is the minimum a Doubao caller needs. Fetched per-workspace
// from workspace_secret rows (feishu_app_id / feishu_access_token /
// feishu_secret_key) by the caller; this package never touches the DB.
type Credentials struct {
	AppID       string
	AccessToken string
	SecretKey   string
}

// Client is the contract the meeting service depends on. Concrete impls:
//
//	doubao.MiaojiClient — POST audio URL → poll until summary ready
//	asr/mock           — used in tests (returns canned SummaryBundle)
type Client interface {
	// BatchSummarize submits the audio at audioURL and polls until the
	// upstream returns a final summary. ctx deadline is honored.
	BatchSummarize(ctx context.Context, creds Credentials, audioURL string) (SummaryBundle, error)
}

// ErrUpstreamNotReady signals BatchSummarize timed out before the
// upstream finished — caller may retry.
var ErrUpstreamNotReady = errors.New("asr upstream not ready")

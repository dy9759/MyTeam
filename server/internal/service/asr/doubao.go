package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxResponseBytes caps any single upstream response we read into
// memory. Result files (transcription / chapter / etc.) can be larger,
// so we cap them separately at maxResultFileBytes.
const (
	maxResponseBytes   = 4 << 20  // 4 MB
	maxResultFileBytes = 32 << 20 // 32 MB — long meetings produce big transcripts
)

// MiaojiClient is the Doubao 妙记 (Lark Minutes) batch impl. Real spec
// per MyIsland/Services/Meeting/MeetingMemoClient.swift + Volcengine
// docs https://www.volcengine.com/docs/6561/1798094.
//
// Flow:
//   POST {Endpoint}/api/v3/auc/lark/submit
//        body: {Input:{Offline:{FileURL,FileType:"audio"}}, Params:{...}}
//        headers: X-Api-App-Key, X-Api-Access-Key, X-Api-Resource-Id,
//                 X-Api-Request-Id (uuid), X-Api-Sequence:-1
//        returns: {TaskID|task_id|...}
//
//   POST {Endpoint}/api/v3/auc/lark/query  (POST, not GET)
//        body: {TaskID:"..."}
//        same headers
//        returns: {Status: "running|success|failed", ResultFileList: [...]}
//
//   For success status: fetch each result file URL and decode the
//   JSON shape (transcription / chapter / information / summary) into
//   the SummaryBundle.
type MiaojiClient struct {
	HTTP       *http.Client
	Endpoint   string        // override for tests; defaults to upstream
	ResourceID string        // ASR resource id, default "volc.lark.minutes"
	PollEvery  time.Duration // poll interval, default 2s
	PollMax    time.Duration // total deadline, default 30min
}

func NewMiaojiClient() *MiaojiClient {
	return &MiaojiClient{
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		Endpoint:   "https://openspeech.bytedance.com",
		ResourceID: "volc.lark.minutes",
		PollEvery:  2 * time.Second,
		// Doubao transcription can take ~1 min per 10 min audio; a 30 min
		// cap covers multi-hour meetings with margin. The previous 5 min
		// cap timed out long recordings (>30 min audio) before upstream
		// finished transcription. See issue #64.
		PollMax: 30 * time.Minute,
	}
}

// submitRequest mirrors the Volcengine spec. Params omitted entirely
// when nil — for now we always send the same Params block (defined
// in defaultParams) so the upstream extracts summary + todos + Q&A.
type submitRequest struct {
	Input  submitInput            `json:"Input"`
	Params map[string]interface{} `json:"Params"`
}
type submitInput struct {
	Offline submitOffline `json:"Offline"`
}
type submitOffline struct {
	FileURL  string `json:"FileURL"`
	FileType string `json:"FileType"`
}

// defaultParams replicates MyIsland's MeetingMemoClient buildSubmitBody.
func defaultParams(topic string) map[string]interface{} {
	return map[string]interface{}{
		"AllActivate":              false,
		"SourceLang":               "zh_cn",
		"AudioTranscriptionEnable": true,
		"AudioTranscriptionParams": map[string]interface{}{
			"SpeakerIdentification": true,
			"NumberOfSpeaker":       0,
		},
		"InformationExtractionEnabled": true,
		"InformationExtractionParams": map[string]interface{}{
			"Types": []string{"todo_list", "question_answer"},
		},
		"SummarizationEnabled": true,
		"SummarizationParams": map[string]interface{}{
			"Types": []string{"summary"},
		},
		"ChapterEnabled": true,
		"Topic":          topic,
	}
}

// genericResp lets us probe for TaskID / Status across the camelCase /
// snake_case variants the upstream uses depending on endpoint version.
type genericResp map[string]interface{}

func (c *MiaojiClient) BatchSummarize(ctx context.Context, creds Credentials, audioURL string) (SummaryBundle, error) {
	taskID, err := c.submit(ctx, creds, audioURL)
	if err != nil {
		return SummaryBundle{}, fmt.Errorf("submit: %w", err)
	}

	deadline := time.Now().Add(c.PollMax)
	for time.Now().Before(deadline) {
		bundle, ready, err := c.query(ctx, creds, taskID)
		if err != nil {
			return SummaryBundle{}, fmt.Errorf("query: %w", err)
		}
		if ready {
			return bundle, nil
		}
		select {
		case <-ctx.Done():
			return SummaryBundle{}, ctx.Err()
		case <-time.After(c.PollEvery):
		}
	}
	return SummaryBundle{}, ErrUpstreamNotReady
}

func (c *MiaojiClient) submit(ctx context.Context, creds Credentials, audioURL string) (string, error) {
	body, _ := json.Marshal(submitRequest{
		Input: submitInput{
			Offline: submitOffline{
				FileURL:  audioURL,
				FileType: "audio",
			},
		},
		Params: defaultParams("meeting"),
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.Endpoint+"/api/v3/auc/lark/submit", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.applyHeaders(req, creds)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("submit http %d: %s",
			resp.StatusCode, redactedBody(raw, creds))
	}
	var g genericResp
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode submit: %w (body=%s)", err, redactedBody(raw, creds))
	}
	if id := pickStringDeep(g, "TaskID", "task_id", "taskId", "job_id", "jobId", "id"); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("submit returned no TaskID (body=%s)", redactedBody(raw, creds))
}

// pickStringDeep walks nested maps to find any of the supplied keys.
// Doubao 妙记 wraps responses inside {"Data":{...}} for some API
// versions; this lets us probe top-level + one level deep without
// hard-coding the wrapper key.
func pickStringDeep(m map[string]interface{}, keys ...string) string {
	if s := pickString(m, keys...); s != "" {
		return s
	}
	for _, v := range m {
		if inner, ok := v.(map[string]interface{}); ok {
			if s := pickString(inner, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

// query POSTs the TaskID and parses the polled status. ready=true only
// when status is success/done/completed AND result files were
// successfully fetched + decoded.
func (c *MiaojiClient) query(ctx context.Context, creds Credentials, taskID string) (SummaryBundle, bool, error) {
	body, _ := json.Marshal(map[string]string{"TaskID": taskID})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.Endpoint+"/api/v3/auc/lark/query", bytes.NewReader(body))
	if err != nil {
		return SummaryBundle{}, false, err
	}
	c.applyHeaders(req, creds)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return SummaryBundle{}, false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode/100 != 2 {
		return SummaryBundle{}, false, fmt.Errorf("query http %d: %s",
			resp.StatusCode, redactedBody(raw, creds))
	}
	var g genericResp
	if err := json.Unmarshal(raw, &g); err != nil {
		return SummaryBundle{}, false, fmt.Errorf("decode query: %w", err)
	}
	status := strings.ToLower(pickStringDeep(g, "Status", "status", "state", "task_status"))
	switch status {
	case "success", "done", "completed":
		bundle, err := c.fetchResults(ctx, g)
		if err != nil {
			return SummaryBundle{}, false, fmt.Errorf("fetch results: %w", err)
		}
		return bundle, true, nil
	case "failed", "error":
		msg := pickString(g, "ErrMessage", "error", "message", "reason")
		return SummaryBundle{}, false, fmt.Errorf("upstream task failed: %s", msg)
	default:
		// queued / running / processing — keep polling
		return SummaryBundle{}, false, nil
	}
}

// fetchResults downloads each result-file URL and merges them into
// one SummaryBundle. Result files are JSON; their internal shape
// varies by extraction pipeline (transcription / chapter /
// information / summary).
func (c *MiaojiClient) fetchResults(ctx context.Context, g genericResp) (SummaryBundle, error) {
	bundle := SummaryBundle{Provider: "doubao_miaoji"}
	files := extractResultFiles(g)

	for _, f := range files {
		if f.URL == "" {
			continue
		}
		bytesRaw, err := c.downloadBytes(ctx, f.URL)
		if err != nil {
			// Don't fail the whole bundle on one missing result; the
			// upstream sometimes returns partial sets when a
			// pipeline didn't run.
			continue
		}
		switch strings.ToLower(f.Kind) {
		case "transcription", "transcript":
			bundle.Segments = append(bundle.Segments, parseTranscription(bytesRaw)...)
		case "chapter":
			var m genericResp
			if err := json.Unmarshal(bytesRaw, &m); err == nil {
				bundle.Sections = append(bundle.Sections, parseChapter(m)...)
			}
		case "information":
			var m genericResp
			if err := json.Unmarshal(bytesRaw, &m); err == nil {
				items, decisions := parseInformation(m)
				bundle.ActionItems = append(bundle.ActionItems, items...)
				bundle.Decisions = append(bundle.Decisions, decisions...)
			}
		case "summary":
			var m genericResp
			if err := json.Unmarshal(bytesRaw, &m); err == nil {
				bundle.Sections = append(bundle.Sections, parseSummary(m)...)
			}
		}
	}
	return bundle, nil
}

type resultFile struct {
	Kind string // "transcription" | "chapter" | "information" | "summary"
	URL  string
}

// extractResultFiles flexibly walks the genericResp looking for keys
// that look like *URL pointing at a result file. Doubao returns these
// in different shapes (top-level keys, nested under Result, etc.).
func extractResultFiles(g genericResp) []resultFile {
	var out []resultFile
	keys := map[string]string{
		// Real Doubao 妙记 keys (per upstream dump 2026-04-19):
		"AudioTranscriptionFile":    "transcription",
		"ChapterFile":               "chapter",
		"InformationExtractionFile": "information",
		"SummarizationFile":         "summary",
		// Older / alt names, kept for forward-compat:
		"TranscriptionURL":  "transcription",
		"transcription_url": "transcription",
		"ChapterURL":        "chapter",
		"chapter_url":       "chapter",
		"InformationURL":    "information",
		"information_url":   "information",
		"SummaryURL":        "summary",
		"summary_url":       "summary",
	}
	walkURLs(g, keys, &out)
	return out
}

func walkURLs(v interface{}, keys map[string]string, out *[]resultFile) {
	// Normalize genericResp (defined type) to its underlying map so
	// the type switch below matches.
	if g, ok := v.(genericResp); ok {
		v = map[string]interface{}(g)
	}
	switch val := v.(type) {
	case map[string]interface{}:
		for k, vv := range val {
			if kind, ok := keys[k]; ok {
				if s, ok := vv.(string); ok && s != "" {
					*out = append(*out, resultFile{Kind: kind, URL: s})
				}
			}
			walkURLs(vv, keys, out)
		}
	case []interface{}:
		for _, e := range val {
			walkURLs(e, keys, out)
		}
	}
}

func (c *MiaojiClient) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResultFileBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("result http %d", resp.StatusCode)
	}
	return raw, nil
}

// parseTranscription pulls speaker / text / start / end from a
// transcription result file. Real Doubao 妙记 returns a top-level
// JSON array of utterances; older specs use {"utterances":[...]}
// or {"sentences":[...]}. Try both shapes.
func parseTranscription(raw []byte) []Segment {
	var segs []Segment

	// Shape A: top-level array.
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, m := range arr {
			segs = append(segs, segmentFromMap(m))
		}
		return segs
	}
	// Shape B: object with utterances / sentences.
	var g genericResp
	if err := json.Unmarshal(raw, &g); err == nil {
		for _, key := range []string{"utterances", "sentences", "Utterances", "Sentences"} {
			if list, ok := g[key].([]interface{}); ok {
				for _, e := range list {
					if m, ok := e.(map[string]interface{}); ok {
						segs = append(segs, segmentFromMap(m))
					}
				}
			}
		}
	}
	return segs
}

func segmentFromMap(m map[string]interface{}) Segment {
	return Segment{
		Speaker: Speaker(pickString(m, "speaker", "Speaker", "speaker_id", "spk")),
		Text:    pickString(m, "text", "Text", "content", "transcript"),
		Start:   time.Duration(pickInt(m, "start_time", "StartTime", "start_ms", "start")) * time.Millisecond,
		End:     time.Duration(pickInt(m, "end_time", "EndTime", "end_ms", "end")) * time.Millisecond,
	}
}

func parseChapter(g genericResp) []string {
	var out []string
	// Real Doubao 妙记 key is "chapter_summary" (per upstream dump).
	for _, key := range []string{"chapter_summary", "chapters", "Chapters", "items"} {
		if list, ok := g[key].([]interface{}); ok {
			for _, e := range list {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				if title := pickString(m, "title", "Title", "name"); title != "" {
					if summary := pickString(m, "summary", "Summary"); summary != "" {
						out = append(out, title+" — "+summary)
					} else {
						out = append(out, title)
					}
				}
			}
		}
	}
	return out
}

func parseSummary(g genericResp) []string {
	var out []string
	// Real Doubao 妙记 key is "paragraph" + "title" (per upstream dump).
	if title := pickString(g, "title", "Title"); title != "" {
		out = append(out, title)
	}
	if p := pickString(g, "paragraph", "Paragraph", "summary", "Summary", "text"); p != "" {
		out = append(out, p)
	}
	for _, key := range []string{"items", "summaries"} {
		if list, ok := g[key].([]interface{}); ok {
			for _, e := range list {
				if s, ok := e.(string); ok {
					out = append(out, s)
				} else if m, ok := e.(map[string]interface{}); ok {
					if s := pickString(m, "summary", "text", "content"); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	}
	return out
}

// parseInformation extracts todo_list (→ ActionItems) and decisions
// from the information result file.
func parseInformation(g genericResp) ([]ActionItem, []string) {
	var items []ActionItem
	var decisions []string

	// todo_list / TodoList / actions
	for _, key := range []string{"todo_list", "TodoList", "todos", "Todos", "actions", "Actions"} {
		if list, ok := g[key].([]interface{}); ok {
			for _, e := range list {
				m, _ := e.(map[string]interface{})
				if m == nil {
					continue
				}
				items = append(items, ActionItem{
					ID:         uuid.NewString(),
					Task:       pickString(m, "task", "content", "todo", "Content"),
					Owner:      pickString(m, "owner", "assignee", "Owner"),
					DueDate:    pickString(m, "due_date", "DueDate", "deadline"),
					Confidence: 0.5, // upstream rarely provides; default neutral
				})
			}
		}
	}

	// decisions / question_answer
	for _, key := range []string{"decisions", "Decisions"} {
		if list, ok := g[key].([]interface{}); ok {
			for _, e := range list {
				if s, ok := e.(string); ok {
					decisions = append(decisions, s)
				} else if m, ok := e.(map[string]interface{}); ok {
					if s := pickString(m, "decision", "text", "content"); s != "" {
						decisions = append(decisions, s)
					}
				}
			}
		}
	}
	return items, decisions
}

func (c *MiaojiClient) applyHeaders(req *http.Request, creds Credentials) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-App-Key", creds.AppID)
	req.Header.Set("X-Api-Access-Key", creds.AccessToken)
	req.Header.Set("X-Api-Resource-Id", c.ResourceID)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
	req.Header.Set("X-Api-Sequence", "-1")
}

// redactedBody truncates the body for inclusion in error messages and
// strips any substring matching the credentials. Some misbehaving
// proxies echo request headers in error responses; without this scrub
// the access token would land in structured logs via wrapped errors.
func redactedBody(raw []byte, creds Credentials) string {
	const maxLen = 512
	s := string(raw)
	if creds.AccessToken != "" {
		s = strings.ReplaceAll(s, creds.AccessToken, "[REDACTED-TOKEN]")
	}
	if creds.SecretKey != "" {
		s = strings.ReplaceAll(s, creds.SecretKey, "[REDACTED-SECRET]")
	}
	if len(s) > maxLen {
		s = s[:maxLen] + "...[truncated]"
	}
	return s
}

// pickString returns the first non-empty string value found at any
// of the supplied keys. Used to span camelCase / snake_case variants
// of the same upstream field.
func pickString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch s := v.(type) {
			case string:
				if s != "" {
					return s
				}
			case map[string]interface{}:
				// Some Doubao responses wrap the value under {"S": "..."}.
				if inner, ok := s["S"].(string); ok && inner != "" {
					return inner
				}
			}
		}
	}
	return ""
}

func pickInt(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return int64(n)
			case int:
				return int64(n)
			case int64:
				return n
			case string:
				var x int64
				_, _ = fmt.Sscanf(n, "%d", &x)
				return x
			}
		}
	}
	return 0
}

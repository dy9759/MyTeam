// Package service — meeting_transcriber.go.
//
// Wraps the Doubao "lark minutes" memo API (openspeech.bytedance.com
// /api/v3/auc/lark/submit + /query). Ported from MyIsland's
// MeetingMemoClient with the exact request shape that succeeded in
// production.
//
// Flow (async, background goroutine):
//
//  1. Caller writes `meeting.audio_url` via UpdateMeetingRecording.
//  2. TranscribeAsync kicks off a goroutine that:
//     - POSTs /submit with the audio URL + topic → TaskID.
//     - Polls /query every 10s until status=success/failed.
//     - Fetches each returned result URL (transcription, summary,
//       chapter, information) into a single JSONB blob.
//     - Writes back via UpdateMeetingResult.
//  3. On failure, records failure_reason + flips status=failed.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// DoubaoMemoConfig mirrors the static credentials + endpoint list
// MyIsland stores per-user. Populated from env at service construction
// so the handler layer never handles secrets directly.
type DoubaoMemoConfig struct {
	SubmitURL   string
	QueryURL    string
	AppID       string
	AccessToken string
	ResourceID  string
}

func LoadDoubaoMemoConfigFromEnv() DoubaoMemoConfig {
	cfg := DoubaoMemoConfig{
		SubmitURL:   os.Getenv("MYTEAM_DOUBAO_MEMO_SUBMIT_URL"),
		QueryURL:    os.Getenv("MYTEAM_DOUBAO_MEMO_QUERY_URL"),
		AppID:       os.Getenv("MYTEAM_DOUBAO_APP_ID"),
		AccessToken: os.Getenv("MYTEAM_DOUBAO_ACCESS_TOKEN"),
		ResourceID:  os.Getenv("MYTEAM_DOUBAO_RESOURCE_ID"),
	}
	if cfg.SubmitURL == "" {
		cfg.SubmitURL = "https://openspeech.bytedance.com/api/v3/auc/lark/submit"
	}
	if cfg.QueryURL == "" {
		cfg.QueryURL = "https://openspeech.bytedance.com/api/v3/auc/lark/query"
	}
	if cfg.ResourceID == "" {
		cfg.ResourceID = "volc.lark.minutes"
	}
	return cfg
}

func (c DoubaoMemoConfig) IsConfigured() bool {
	return c.AppID != "" && c.AccessToken != ""
}

// MeetingTranscriber owns the background transcription pipeline.
// Hub is optional — nil skips the WS push (tests).
type MeetingTranscriber struct {
	Q       *db.Queries
	Cfg     DoubaoMemoConfig
	Client  *http.Client
	Publish func(workspaceID string, eventType string, payload map[string]any)
}

func NewMeetingTranscriber(
	q *db.Queries,
	cfg DoubaoMemoConfig,
	publish func(workspaceID string, eventType string, payload map[string]any),
) *MeetingTranscriber {
	return &MeetingTranscriber{
		Q:   q,
		Cfg: cfg,
		// 180s per-request: Doubao /query and /submit occasionally take
		// 60-120s under load. Tighter than that causes false-positive
		// "context deadline exceeded" failures that kill a perfectly
		// healthy polling loop after one blip.
		Client:  &http.Client{Timeout: 180 * time.Second},
		Publish: publish,
	}
}

// TranscribeAsync schedules a background job that submits the
// meeting's audio to Doubao and writes the result back into the row.
// It's intentionally fire-and-forget — the UI watches the row via
// polling + WS events.
func (t *MeetingTranscriber) TranscribeAsync(meetingID pgtype.UUID, audioURL, topic, workspaceID string) {
	if !t.Cfg.IsConfigured() {
		slog.Warn("meeting: Doubao not configured, skipping transcription",
			"meeting_id", uuidString(meetingID))
		_, _ = t.Q.UpdateMeetingResult(context.Background(), db.UpdateMeetingResultParams{
			ID:            meetingID,
			Status:        "failed",
			FailureReason: pgtype.Text{String: "Doubao memo API not configured", Valid: true},
		})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		if err := t.runTranscription(ctx, meetingID, audioURL, topic, workspaceID); err != nil {
			slog.Warn("meeting: transcription failed",
				"meeting_id", uuidString(meetingID), "err", err)
			_, _ = t.Q.UpdateMeetingResult(ctx, db.UpdateMeetingResultParams{
				ID:            meetingID,
				Status:        "failed",
				FailureReason: pgtype.Text{String: err.Error(), Valid: true},
			})
			if t.Publish != nil {
				t.Publish(workspaceID, "meeting:failed", map[string]any{
					"meeting_id": uuidString(meetingID),
					"error":      err.Error(),
				})
			}
		}
	}()
}

func (t *MeetingTranscriber) runTranscription(
	ctx context.Context,
	meetingID pgtype.UUID,
	audioURL, topic, workspaceID string,
) error {
	taskID, err := t.submit(ctx, audioURL, topic)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	if err := t.Q.UpdateMeetingTaskID(ctx, db.UpdateMeetingTaskIDParams{
		ID:     meetingID,
		TaskID: pgtype.Text{String: taskID, Valid: true},
	}); err != nil {
		slog.Warn("meeting: persist task id failed", "err", err)
	}

	if t.Publish != nil {
		t.Publish(workspaceID, "meeting:progress", map[string]any{
			"meeting_id":    uuidString(meetingID),
			"task_id":       taskID,
			"attempt":       0,
			"elapsed_ms":    int64(0),
			"doubao_status": "submitted",
		})
	}

	transcript, summary, err := t.pollResult(ctx, meetingID, workspaceID, taskID)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	transcriptJSON, _ := json.Marshal(transcript)
	summaryJSON, _ := json.Marshal(summary)

	if _, err := t.Q.UpdateMeetingResult(ctx, db.UpdateMeetingResultParams{
		ID:         meetingID,
		Status:     "completed",
		Transcript: transcriptJSON,
		Summary:    summaryJSON,
	}); err != nil {
		return fmt.Errorf("persist result: %w", err)
	}

	if t.Publish != nil {
		t.Publish(workspaceID, "meeting:completed", map[string]any{
			"meeting_id": uuidString(meetingID),
		})
	}
	return nil
}

func (t *MeetingTranscriber) submit(ctx context.Context, audioURL, topic string) (string, error) {
	body := map[string]any{
		"Input": map[string]any{
			"Offline": map[string]any{
				"FileURL":  audioURL,
				"FileType": "audio",
			},
		},
		"Params": map[string]any{
			"AllActivate":              false,
			"SourceLang":               "zh_cn",
			"AudioTranscriptionEnable": true,
			"AudioTranscriptionParams": map[string]any{
				"SpeakerIdentification": true,
				"NumberOfSpeaker":       0,
			},
			"InformationExtractionEnabled": true,
			"InformationExtractionParams": map[string]any{
				"Types": []string{"todo_list", "question_answer"},
			},
			"SummarizationEnabled": true,
			"SummarizationParams": map[string]any{
				"Types": []string{"summary"},
			},
			"ChapterEnabled": true,
			"Topic":          topic,
		},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", t.Cfg.SubmitURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	t.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	// Doubao carries business status in response headers, not HTTP status.
	// Always surface them alongside body so failures are diagnosable.
	apiStatus := resp.Header.Get("X-Api-Status-Code")
	apiMsg := resp.Header.Get("X-Api-Message")
	logID := resp.Header.Get("X-Tt-Logid")
	bodySnippet := truncateForLog(string(data), 1024)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("meeting: submit http non-2xx",
			"http_status", resp.StatusCode,
			"x_api_status_code", apiStatus,
			"x_api_message", apiMsg,
			"x_tt_logid", logID,
			"body", bodySnippet)
		return "", fmt.Errorf("submit http %d (api_status=%q api_msg=%q logid=%q): %s",
			resp.StatusCode, apiStatus, apiMsg, logID, bodySnippet)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		slog.Warn("meeting: submit decode failed",
			"x_api_status_code", apiStatus,
			"x_api_message", apiMsg,
			"x_tt_logid", logID,
			"body", bodySnippet,
			"err", err)
		return "", fmt.Errorf("submit decode (api_status=%q api_msg=%q logid=%q body=%s): %w",
			apiStatus, apiMsg, logID, bodySnippet, err)
	}
	if id := extractTaskID(parsed); id != "" {
		return id, nil
	}
	slog.Warn("meeting: submit no TaskID in response",
		"x_api_status_code", apiStatus,
		"x_api_message", apiMsg,
		"x_tt_logid", logID,
		"body", bodySnippet)
	return "", fmt.Errorf("no TaskID in submit response (api_status=%q api_msg=%q logid=%q): %s",
		apiStatus, apiMsg, logID, bodySnippet)
}

// extractTaskID looks for Doubao's TaskID at the top level and inside
// the common "Result"/"Data" envelopes. Returns "" when not present.
func extractTaskID(m map[string]any) string {
	keys := []string{"TaskID", "task_id", "taskId", "id"}
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	for _, envelope := range []string{"Result", "result", "Data", "data"} {
		if nested, ok := m[envelope].(map[string]any); ok {
			for _, k := range keys {
				if v, ok := nested[k].(string); ok && v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// flattenDoubaoResult fetches every *File URL in the Doubao /query
// Result envelope and merges the contents into two JSON-friendly maps
// the frontend can render directly.
//
// Doubao returns short-lived signed TOS URLs pointing at per-section
// JSON blobs (transcript sentences, summary paragraph, chapter list,
// Q&A and todos). Each URL is private to the service account and
// expires in ~24h, so we inline the content at completion time to
// avoid dead links and extra browser round-trips.
func flattenDoubaoResult(ctx context.Context, client *http.Client, result map[string]any) (transcript, summary map[string]any) {
	summary = map[string]any{}
	if result == nil {
		return nil, summary
	}

	// File URLs Doubao returns. Missing / empty fields are skipped.
	fetches := map[string]string{
		"transcript":  pickStr(result, "AudioTranscriptionFile", "audio_transcription_file"),
		"summary":     pickStr(result, "SummarizationFile", "summarization_file"),
		"chapter":     pickStr(result, "ChapterFile", "chapter_file"),
		"info":        pickStr(result, "InformationExtractionFile", "information_extraction_file"),
		"translation": pickStr(result, "TranslationFile", "translation_file"),
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok = map[string][]byte{}
	)
	for k, u := range fetches {
		if u == "" {
			continue
		}
		wg.Add(1)
		go func(key, url string) {
			defer wg.Done()
			reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
			if err != nil {
				slog.Warn("meeting: build result fetch req", "key", key, "err", err)
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				slog.Warn("meeting: fetch result file", "key", key, "err", err)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				slog.Warn("meeting: read result body", "key", key, "err", err)
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				slog.Warn("meeting: result file non-2xx",
					"key", key,
					"http_status", resp.StatusCode,
					"body", truncateForLog(string(body), 256))
				return
			}
			mu.Lock()
			ok[key] = body
			mu.Unlock()
		}(k, u)
	}
	wg.Wait()

	// transcript: {sentences: [...]}. Doubao returns a bare array.
	if raw := ok["transcript"]; len(raw) > 0 {
		var sentences []any
		if err := json.Unmarshal(raw, &sentences); err == nil {
			transcript = map[string]any{"sentences": sentences}
		} else {
			// Defensive — if Doubao ever switches to object shape, keep
			// the raw form instead of dropping content on the floor.
			var anyShape any
			_ = json.Unmarshal(raw, &anyShape)
			transcript = map[string]any{"raw": anyShape}
		}
	}

	// summary section — paragraph + title
	if raw := ok["summary"]; len(raw) > 0 {
		var s struct {
			Paragraph string `json:"paragraph"`
			Title     string `json:"title"`
		}
		if err := json.Unmarshal(raw, &s); err == nil {
			summary["summary"] = s.Paragraph
			summary["title"] = s.Title
		}
	}
	// chapters
	if raw := ok["chapter"]; len(raw) > 0 {
		var c struct {
			ChapterSummary []any `json:"chapter_summary"`
		}
		if err := json.Unmarshal(raw, &c); err == nil {
			summary["chapters"] = c.ChapterSummary
		}
	}
	// information extraction: Q&A + todos
	if raw := ok["info"]; len(raw) > 0 {
		var info struct {
			QuestionAnswer []any `json:"question_answer"`
			TodoList       []any `json:"todo_list"`
		}
		if err := json.Unmarshal(raw, &info); err == nil {
			summary["qa"] = info.QuestionAnswer
			summary["todos"] = info.TodoList
		}
	}
	// translation (optional, rare)
	if raw := ok["translation"]; len(raw) > 0 {
		var anyShape any
		if err := json.Unmarshal(raw, &anyShape); err == nil {
			summary["translation"] = anyShape
		}
	}

	// Keep the original URLs around for debugging without expiry logic.
	summary["raw_files"] = result
	return transcript, summary
}

// pickStr reads the first non-empty string value under the listed keys.
func pickStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// sleepWithCtx blocks for d or until ctx is cancelled. Returns true if
// the sleep completed normally, false if ctx fired first.
func sleepWithCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// pollResult drives the Doubao query loop with adaptive backoff and
// per-attempt WS progress events. Total deadline: 30 minutes.
// Interval schedule:
//   - attempts 0..14   → 2s  (first 30s feels near-live)
//   - attempts 15..39  → 5s  (next ~2min, covers typical short clips)
//   - attempts 40+     → 10s (long tail for multi-minute audio)
//
// Progress event `meeting:progress` fires after every poll while the
// task is still running so the UI can show "processing — Ns elapsed"
// without sitting on a blank status.
func (t *MeetingTranscriber) pollResult(
	ctx context.Context,
	meetingID pgtype.UUID,
	workspaceID string,
	taskID string,
) (transcript map[string]any, summary map[string]any, err error) {
	body, _ := json.Marshal(map[string]any{"TaskID": taskID})
	const overallDeadline = 30 * time.Minute
	start := time.Now()
	deadline := start.Add(overallDeadline)

	pickInterval := func(attempt int) time.Duration {
		switch {
		case attempt < 15:
			return 2 * time.Second
		case attempt < 40:
			return 5 * time.Second
		default:
			return 10 * time.Second
		}
	}

	publishProgress := func(attempt int, doubaoStatus string) {
		if t.Publish == nil {
			return
		}
		t.Publish(workspaceID, "meeting:progress", map[string]any{
			"meeting_id":     uuidString(meetingID),
			"task_id":        taskID,
			"attempt":        attempt,
			"elapsed_ms":     time.Since(start).Milliseconds(),
			"doubao_status":  doubaoStatus,
		})
	}

	// Transient-error budget: a single network blip / 5xx shouldn't
	// kill a healthy polling loop. Reset whenever we get a clean 2xx
	// with a parsed status; exhaust = escalate to failure.
	transientLeft := 5

	for attempt := 0; time.Now().Before(deadline); attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", t.Cfg.QueryURL, bytes.NewReader(body))
		if reqErr != nil {
			return nil, nil, reqErr
		}
		t.applyHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := t.Client.Do(req)
		if doErr != nil {
			if transientLeft <= 0 {
				return nil, nil, fmt.Errorf("query transient exhausted: %w", doErr)
			}
			transientLeft--
			slog.Warn("meeting: query network error, retrying",
				"attempt", attempt,
				"transient_left", transientLeft,
				"err", doErr)
			if !sleepWithCtx(ctx, pickInterval(attempt)) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		apiStatus := resp.Header.Get("X-Api-Status-Code")
		apiMsg := resp.Header.Get("X-Api-Message")
		logID := resp.Header.Get("X-Tt-Logid")
		resp.Body.Close()

		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			if transientLeft <= 0 {
				return nil, nil, fmt.Errorf("query 5xx exhausted: http %d body=%s",
					resp.StatusCode, truncateForLog(string(data), 1024))
			}
			transientLeft--
			slog.Warn("meeting: query 5xx, retrying",
				"http_status", resp.StatusCode,
				"x_tt_logid", logID,
				"transient_left", transientLeft)
			if !sleepWithCtx(ctx, pickInterval(attempt)) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodySnippet := truncateForLog(string(data), 1024)
			slog.Warn("meeting: query http non-2xx",
				"http_status", resp.StatusCode,
				"x_api_status_code", apiStatus,
				"x_api_message", apiMsg,
				"x_tt_logid", logID,
				"body", bodySnippet)
			return nil, nil, fmt.Errorf("query http %d (api_status=%q api_msg=%q logid=%q): %s",
				resp.StatusCode, apiStatus, apiMsg, logID, bodySnippet)
		}
		var parsed map[string]any
		if jErr := json.Unmarshal(data, &parsed); jErr != nil {
			return nil, nil, fmt.Errorf("query decode (logid=%q body=%s): %w",
				logID, truncateForLog(string(data), 1024), jErr)
		}
		// Healthy response — reset transient budget.
		transientLeft = 5

		// Doubao wraps everything inside a "Data" envelope, e.g.
		//   {"Data":{"Status":"failed","ErrCode":4020,"ErrMessage":"http error",
		//            "Result":{"Transcription":{...}}, ...}}
		// Accept both the nested form (real API) and a flat form
		// (defensive — keeps older mocks + future shape changes working).
		inner := parsed
		if nested, ok := parsed["Data"].(map[string]any); ok && nested != nil {
			inner = nested
		} else if nested, ok := parsed["data"].(map[string]any); ok && nested != nil {
			inner = nested
		}

		status, _ := inner["Status"].(string)
		if status == "" {
			status, _ = inner["status"].(string)
		}
		errCode := 0
		if v, ok := inner["ErrCode"].(float64); ok {
			errCode = int(v)
		} else if v, ok := inner["err_code"].(float64); ok {
			errCode = int(v)
		}
		slog.Debug("meeting: query poll",
			"attempt", attempt,
			"elapsed_ms", time.Since(start).Milliseconds(),
			"doubao_status", status,
			"err_code", errCode,
			"x_tt_logid", logID)
		switch status {
		case "success", "done", "completed":
			result, _ := inner["Result"].(map[string]any)
			if result == nil {
				result, _ = inner["result"].(map[string]any)
			}
			// Doubao ships actual content as short-TTL signed URLs
			// inside *File fields. Fetch each, flatten into the shapes
			// the frontend SummarySection and TranscriptView expect:
			//
			//   transcript = { sentences: [...] }
			//   summary = {
			//     summary, title, chapters, todos, qa, raw_files
			//   }
			transcript, summary = flattenDoubaoResult(ctx, t.Client, result)
			return transcript, summary, nil
		case "failed", "error":
			msg, _ := inner["ErrMessage"].(string)
			if msg == "" {
				msg, _ = inner["err_message"].(string)
			}
			if msg == "" {
				msg, _ = inner["message"].(string)
			}
			if msg == "" {
				msg = "Doubao returned failure without message"
			}
			return nil, nil, fmt.Errorf("doubao err_code=%d: %s (logid=%s)", errCode, msg, logID)
		}

		publishProgress(attempt, status)

		interval := pickInterval(attempt)
		if remaining := time.Until(deadline); remaining < interval {
			interval = remaining
		}
		if interval <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(interval):
		}
	}
	return nil, nil, fmt.Errorf("poll exhausted (>%s)", overallDeadline)
}

func (t *MeetingTranscriber) applyHeaders(req *http.Request) {
	req.Header.Set("X-Api-App-Key", t.Cfg.AppID)
	req.Header.Set("X-Api-Access-Key", t.Cfg.AccessToken)
	if t.Cfg.ResourceID != "" {
		req.Header.Set("X-Api-Resource-Id", t.Cfg.ResourceID)
	}
	req.Header.Set("X-Api-Request-Id", uuid.New().String())
	req.Header.Set("X-Api-Sequence", "-1")
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

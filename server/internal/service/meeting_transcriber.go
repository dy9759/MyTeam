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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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
		Q:       q,
		Cfg:     cfg,
		Client:  &http.Client{Timeout: 60 * time.Second},
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
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
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

	transcript, summary, err := t.pollResult(ctx, taskID)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("submit status %d: %s", resp.StatusCode, string(data))
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("submit decode: %w", err)
	}
	for _, k := range []string{"TaskID", "task_id", "taskId", "id"} {
		if v, ok := parsed[k].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", errors.New("no TaskID in submit response")
}

func (t *MeetingTranscriber) pollResult(
	ctx context.Context,
	taskID string,
) (transcript map[string]any, summary map[string]any, err error) {
	body, _ := json.Marshal(map[string]any{"TaskID": taskID})
	for attempt := 0; attempt < 60; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", t.Cfg.QueryURL, bytes.NewReader(body))
		if reqErr != nil {
			return nil, nil, reqErr
		}
		t.applyHeaders(req)
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := t.Client.Do(req)
		if doErr != nil {
			return nil, nil, doErr
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, nil, fmt.Errorf("query status %d: %s", resp.StatusCode, string(data))
		}
		var parsed map[string]any
		if jErr := json.Unmarshal(data, &parsed); jErr != nil {
			return nil, nil, fmt.Errorf("query decode: %w", jErr)
		}
		status, _ := parsed["Status"].(string)
		if status == "" {
			status, _ = parsed["status"].(string)
		}
		switch status {
		case "success", "done", "completed":
			// Result schema: {Result: {Transcription:..., Summary:..., ...}}
			result, _ := parsed["Result"].(map[string]any)
			if result == nil {
				result, _ = parsed["result"].(map[string]any)
			}
			transcript, _ = result["Transcription"].(map[string]any)
			if transcript == nil {
				transcript, _ = result["transcription"].(map[string]any)
			}
			summary = result
			return transcript, summary, nil
		case "failed", "error":
			msg, _ := parsed["ErrMessage"].(string)
			if msg == "" {
				msg, _ = parsed["message"].(string)
			}
			if msg == "" {
				msg = "Doubao returned failure without message"
			}
			return nil, nil, errors.New(msg)
		}
		// still running — wait 10s
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return nil, nil, errors.New("poll exhausted (>10 min)")
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

package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// MiaojiClient is the Doubao 妙记 (Lark Audio Understanding) batch impl.
// Two-step flow: POST submit returns a task_id; GET query polls until
// status="success" then returns the structured summary payload.
//
// API surface:
//
//	POST {Endpoint}/api/v3/auc/lark/submit  body: {"url":"...","callback":""}
//	    headers: X-Api-App-Key, X-Api-Access-Key, X-Api-Resource-Id,
//	             X-Api-Request-Id (random uuid)
//	    returns: {"resp":{"id":"<task_id>"}}
//
//	GET  {Endpoint}/api/v3/auc/lark/query?id=<task_id>
//	    same headers
//	    returns: {"resp":{"status":"...","result":{...}}}
type MiaojiClient struct {
	HTTP       *http.Client
	Endpoint   string        // override for tests; defaults to upstream
	ResourceID string        // ASR resource id, defaults to volc.bigasr.auc.lark
	PollEvery  time.Duration // poll interval, default 2s
	PollMax    time.Duration // total deadline, default 5min
}

// NewMiaojiClient returns a client with sensible defaults.
func NewMiaojiClient() *MiaojiClient {
	return &MiaojiClient{
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		Endpoint:   "https://openspeech.bytedance.com",
		ResourceID: "volc.bigasr.auc.lark",
		PollEvery:  2 * time.Second,
		PollMax:    5 * time.Minute,
	}
}

type submitReq struct {
	URL      string `json:"url"`
	Callback string `json:"callback,omitempty"`
}
type submitResp struct {
	Resp struct {
		ID string `json:"id"`
	} `json:"resp"`
}

type queryResp struct {
	Resp struct {
		Status string `json:"status"`
		Result struct {
			Sections    []string `json:"sections"`
			Decisions   []string `json:"decisions"`
			ActionItems []struct {
				Task       string  `json:"task"`
				Owner      string  `json:"owner"`
				DueDate    string  `json:"due_date"`
				Confidence float64 `json:"confidence"`
			} `json:"action_items"`
			Segments []struct {
				Speaker string `json:"speaker"`
				Text    string `json:"text"`
				StartMs int64  `json:"start_ms"`
				EndMs   int64  `json:"end_ms"`
			} `json:"segments"`
		} `json:"result"`
	} `json:"resp"`
}

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
	body, _ := json.Marshal(submitReq{URL: audioURL})
	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint+"/api/v3/auc/lark/submit", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.applyHeaders(req, creds)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("submit http %d: %s", resp.StatusCode, string(raw))
	}
	var sr submitResp
	if err := json.Unmarshal(raw, &sr); err != nil {
		return "", fmt.Errorf("decode submit: %w (body=%s)", err, string(raw))
	}
	if sr.Resp.ID == "" {
		return "", fmt.Errorf("submit returned empty id (body=%s)", string(raw))
	}
	return sr.Resp.ID, nil
}

func (c *MiaojiClient) query(ctx context.Context, creds Credentials, taskID string) (SummaryBundle, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Endpoint+"/api/v3/auc/lark/query?id="+taskID, nil)
	if err != nil {
		return SummaryBundle{}, false, err
	}
	c.applyHeaders(req, creds)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return SummaryBundle{}, false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return SummaryBundle{}, false, fmt.Errorf("query http %d: %s", resp.StatusCode, string(raw))
	}
	var qr queryResp
	if err := json.Unmarshal(raw, &qr); err != nil {
		return SummaryBundle{}, false, fmt.Errorf("decode query: %w", err)
	}
	switch qr.Resp.Status {
	case "success":
		return c.toBundle(qr), true, nil
	case "failed", "error":
		return SummaryBundle{}, false, fmt.Errorf("upstream task failed: %s", qr.Resp.Status)
	default:
		// queued / running / processing — keep polling
		return SummaryBundle{}, false, nil
	}
}

func (c *MiaojiClient) toBundle(qr queryResp) SummaryBundle {
	b := SummaryBundle{
		Provider:  "doubao_miaoji",
		Sections:  qr.Resp.Result.Sections,
		Decisions: qr.Resp.Result.Decisions,
	}
	for _, s := range qr.Resp.Result.Segments {
		b.Segments = append(b.Segments, Segment{
			Speaker: Speaker(s.Speaker),
			Text:    s.Text,
			Start:   time.Duration(s.StartMs) * time.Millisecond,
			End:     time.Duration(s.EndMs) * time.Millisecond,
		})
	}
	for _, ai := range qr.Resp.Result.ActionItems {
		conf := ai.Confidence
		if conf == 0 {
			conf = 0.5
		}
		b.ActionItems = append(b.ActionItems, ActionItem{
			ID:         uuid.NewString(),
			Task:       ai.Task,
			Owner:      ai.Owner,
			DueDate:    ai.DueDate,
			Confidence: conf,
		})
	}
	return b
}

func (c *MiaojiClient) applyHeaders(req *http.Request, creds Credentials) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-App-Key", creds.AppID)
	req.Header.Set("X-Api-Access-Key", creds.AccessToken)
	req.Header.Set("X-Api-Resource-Id", c.ResourceID)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
}

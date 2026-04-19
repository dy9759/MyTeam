// meeting_e2e_test.go — opt-in end-to-end test that exercises real
// Volcengine TOS + real Doubao 妙记. Skips by default; enable via
//
//	MEETING_E2E=1 \
//	TOS_ACCESS_KEY_ID=... TOS_SECRET_ACCESS_KEY=... TOS_BUCKET=... \
//	TOS_REGION=cn-beijing TOS_ENDPOINT=https://tos-s3-cn-beijing.volces.com \
//	FEISHU_APP_ID=... FEISHU_ACCESS_TOKEN=... FEISHU_SECRET_KEY=... \
//	MEETING_E2E_AUDIO=/path/to/sample.mp3 \
//	go test ./internal/service -run TestMeetingService_E2E -v
//
// Credentials are read from env only — never hardcoded, never written
// to disk. Audio file required (no synthetic audio works for 妙记).
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service/asr"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// envSecrets satisfies SecretGetter from process env. Only used for
// the e2e test — production wires the real SecretService that reads
// workspace_secret rows.
type envSecrets struct {
	feishuAppID  string
	feishuToken  string
	feishuSecret string
}

func (e envSecrets) GetPlaintext(_ context.Context, _ uuid.UUID, key string) (string, error) {
	switch key {
	case "feishu_app_id":
		return e.feishuAppID, nil
	case "feishu_access_token":
		return e.feishuToken, nil
	case "feishu_secret_key":
		return e.feishuSecret, nil
	}
	return "", nil
}

func TestMeetingService_E2E_TOSPlusDoubao(t *testing.T) {
	if os.Getenv("MEETING_E2E") != "1" {
		t.Skip("MEETING_E2E!=1; opt in to run real-service e2e")
	}
	for _, k := range []string{
		"TOS_ACCESS_KEY_ID", "TOS_SECRET_ACCESS_KEY", "TOS_BUCKET",
		"FEISHU_APP_ID", "FEISHU_ACCESS_TOKEN", "FEISHU_SECRET_KEY",
		"MEETING_E2E_AUDIO",
	} {
		if os.Getenv(k) == "" {
			t.Fatalf("required env %s not set", k)
		}
	}
	audioPath := os.Getenv("MEETING_E2E_AUDIO")
	audioBytes, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read audio %q: %v", audioPath, err)
	}

	q := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// Build TOS storage from env.
	tos, err := storage.NewTOSStorage(ctx, storage.TOSConfig{
		AccessKeyID:     os.Getenv("TOS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("TOS_SECRET_ACCESS_KEY"),
		Bucket:          os.Getenv("TOS_BUCKET"),
		Region:          e2eEnvOr("TOS_REGION", "cn-beijing"),
		Endpoint:        e2eEnvOr("TOS_ENDPOINT", ""),
	})
	if err != nil {
		t.Fatalf("tos init: %v", err)
	}

	// Workspace + member + channel + thread (re-use service test
	// fixture helpers).
	wsID := createTestWorkspace(t, q)
	userID := createTestUser(t, q, "e2e@example.com", "E2E User")
	if _, err := q.CreateMember(ctx, db.CreateMemberParams{WorkspaceID: wsID, UserID: userID, Role: "owner"}); err != nil {
		t.Fatalf("member: %v", err)
	}
	ch, err := q.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID:   wsID,
		Name:          "e2e-" + uuid.NewString()[:8],
		CreatedBy:     userID,
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	thread, err := q.CreateThread(ctx, db.CreateThreadParams{
		ChannelID:     ch.ID,
		WorkspaceID:   wsID,
		Title:         pgtype.Text{String: "E2E meeting", Valid: true},
		Status:        pgtype.Text{String: "active", Valid: true},
		CreatedBy:     userID,
		CreatedByType: pgtype.Text{String: "member", Valid: true},
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	// Real Doubao client (default endpoint + 5-min poll budget).
	doubao := asr.NewMiaojiClient()
	doubao.PollMax = 4 * time.Minute

	svc := NewMeetingService(q, openTestPool(t),
		envSecrets{
			feishuAppID:  os.Getenv("FEISHU_APP_ID"),
			feishuToken:  os.Getenv("FEISHU_ACCESS_TOKEN"),
			feishuSecret: os.Getenv("FEISHU_SECRET_KEY"),
		},
		doubao,
	).WithStorage(tos)

	threadID := uuid.UUID(thread.ID.Bytes)
	if _, err := svc.StartMeeting(ctx, threadID, []string{"e2e walkthrough"}); err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}

	// Upload to TOS via Storage interface; sets audio_file_id +
	// status=transcribing.
	meta, fileID, err := svc.UploadAudio(ctx, threadID, uuid.UUID(userID.Bytes),
		bytes.NewReader(audioBytes), "sample.mp3", "audio/mpeg", int64(len(audioBytes)))
	if err != nil {
		t.Fatalf("UploadAudio: %v", err)
	}
	t.Logf("uploaded: file_id=%s storage_path stored on file_index", fileID)
	if meta.MeetingStatus != MeetingTranscribing {
		t.Fatalf("status after upload: %s", meta.MeetingStatus)
	}

	// Summarize: empty audioURL → service auto-presigns from
	// file_index.storage_path → calls Doubao.
	bundle, err := svc.Summarize(ctx, threadID, "")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	t.Logf("doubao returned: provider=%s sections=%d decisions=%d action_items=%d segments=%d",
		bundle.Provider, len(bundle.Sections), len(bundle.Decisions),
		len(bundle.ActionItems), len(bundle.Segments))
	for i, ai := range bundle.ActionItems {
		t.Logf("  action %d: task=%q owner=%q confidence=%.2f", i, ai.Task, ai.Owner, ai.Confidence)
	}

	// Verify thread.metadata reflects the post-summary state.
	row, err := q.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("reload thread: %v", err)
	}
	var stored MeetingMeta
	if err := json.Unmarshal(row.Metadata, &stored); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if stored.MeetingStatus != MeetingSummarized {
		t.Errorf("final status: want summarized, got %s", stored.MeetingStatus)
	}
	if stored.ASRProvider == "" {
		t.Errorf("asr_provider empty")
	}
}

func e2eEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}


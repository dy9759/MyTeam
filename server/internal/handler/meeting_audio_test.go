package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MyAIOSHub/MyTeam/server/internal/middleware"
)

// TestIsAllowedAudioMIME — whitelist gate for the audio upload endpoint.
// Issue #62.
func TestIsAllowedAudioMIME(t *testing.T) {
	cases := map[string]bool{
		"audio/mpeg":                 true,
		"audio/mp4":                  true,
		"audio/wav":                  true,
		"audio/webm":                 true,
		"audio/ogg":                  true,
		"audio/flac":                 true,
		"audio/aac":                  true,
		"AUDIO/MPEG":                 true, // case-insensitive
		"audio/mpeg; charset=binary": true, // strip params
		"":                           false,
		"audio/":                     false,
		"text/plain":                 false,
		"image/png":                  false,
		"application/octet-stream":   false,
		"video/mp4":                  false,
	}
	for ct, want := range cases {
		if got := isAllowedAudioMIME(ct); got != want {
			t.Errorf("isAllowedAudioMIME(%q) = %v, want %v", ct, got, want)
		}
	}
}

// TestUploadMeetingAudio_RejectsBadMIME — reject non-audio MIME at the
// IPC boundary even when the multipart shape is correct. Issue #62.
func TestUploadMeetingAudio_RejectsBadMIME(t *testing.T) {
	fx := newMeetingFixture(t)
	t.Cleanup(fx.cleanup)
	withMeetingDeps(t, &handlerFakeASR{})

	// Start the meeting so kind=meeting; upload then proceeds past
	// the fixture-shape checks and hits the MIME gate.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/threads/"+fx.threadID+"/meeting/start", nil)
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.StartMeeting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartMeeting setup: %d %s", w.Code, w.Body.String())
	}

	// Build a multipart body with file Content-Type=text/plain.
	body, ct := buildMultipartUpload(t, "evil.mp3", "text/plain", []byte("not audio"))
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/threads/"+fx.threadID+"/meeting/audio", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-User-ID", testUserID)
	// resolveWorkspaceID reads only the middleware-injected context after
	// the #20 fix; mirror that here since this request bypasses newRequest.
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, testMember))
	req = withURLParam(req, "threadID", fx.threadID)
	testHandler.UploadMeetingAudio(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "audio/") {
		t.Errorf("error message should mention audio/* — got %s", w.Body.String())
	}
}

// buildMultipartUpload returns a body + content-type header for a
// single-file multipart/form-data POST. The file part advertises the
// supplied Content-Type so we can exercise the MIME gate.
func buildMultipartUpload(t *testing.T, filename, contentType string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{
		`form-data; name="file"; filename="` + filename + `"`,
	}
	hdr["Content-Type"] = []string{contentType}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	return body, mw.FormDataContentType()
}

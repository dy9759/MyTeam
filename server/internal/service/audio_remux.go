// Package service — audio_remux.go.
//
// Thin wrapper around ffmpeg that repackages a WebM/Opus audio blob
// into an Ogg/Opus container without re-encoding. Doubao's lark-memo
// API does not accept WebM; it does accept ogg-opus. Since the browser's
// MediaRecorder emits WebM/Opus but the codec is the same as Ogg's,
// `ffmpeg -c:a copy` gives a bit-for-bit compatible audio stream in a
// container Doubao understands — no quality loss, no CPU re-encode cost.
//
// Fails closed: if ffmpeg is missing or remux errors, the caller should
// fall back to the original bytes rather than silently dropping audio.
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var (
	ffmpegOnce     sync.Once
	ffmpegPath     string
	ffmpegFindErr  error
)

// FFmpegAvailable reports whether ffmpeg is resolvable on PATH. Cached
// after the first lookup because `exec.LookPath` traverses $PATH on
// each call.
func FFmpegAvailable() bool {
	ffmpegOnce.Do(func() {
		ffmpegPath, ffmpegFindErr = exec.LookPath("ffmpeg")
	})
	return ffmpegFindErr == nil && ffmpegPath != ""
}

// RemuxWebMToOgg pipes the input WebM bytes through ffmpeg and returns
// the Ogg/Opus output. Context governs the ffmpeg process lifetime.
// A separate 30s hard timeout guards against runaway processes even
// when callers pass context.Background.
func RemuxWebMToOgg(ctx context.Context, webm []byte) ([]byte, error) {
	if !FFmpegAvailable() {
		return nil, errors.New("ffmpeg not available on PATH")
	}
	if len(webm) == 0 {
		return nil, errors.New("empty input")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-f", "webm", // explicit demuxer — piped stdin skips auto-detect
		"-i", "pipe:0",
		"-c:a", "copy",
		"-map_metadata", "-1",
		"-f", "ogg",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(webm)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg remux: %w: %s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, errors.New("ffmpeg produced empty ogg output")
	}
	return stdout.Bytes(), nil
}

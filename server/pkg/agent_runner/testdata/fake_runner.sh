#!/bin/bash
# fake_runner.sh — deterministic stand-in for claude_reply.py, used by runner_test.go.
# Mode is passed as first arg; prompt is read from stdin.
set -u

MODE="${1:-success}"
PROMPT="$(cat)"

case "$MODE" in
  success)
    printf '{"type":"done","text":"echo: %s"}\n' "$PROMPT"
    exit 0
    ;;
  error)
    printf '{"type":"error","message":"missing sdk"}\n'
    exit 2
    ;;
  nodone)
    printf '{"type":"status","message":"started"}\n'
    exit 0
    ;;
  timeout)
    sleep 10
    ;;
  env_openai)
    printf '{"type":"done","text":"%s|%s|%s"}\n' \
      "${OPENAI_BASE_URL:-}" "${OPENAI_API_KEY:-}" "${OPENAI_MODEL:-}"
    exit 0
    ;;
  env_anthropic)
    printf '{"type":"done","text":"%s|%s|%s"}\n' \
      "${ANTHROPIC_BASE_URL:-}" "${ANTHROPIC_API_KEY:-}" "${ANTHROPIC_MODEL:-}"
    exit 0
    ;;
  noisy)
    printf 'not valid json\n'
    printf '{"type":"done","text":"ok"}\n'
    exit 0
    ;;
  *)
    printf '{"type":"error","message":"unknown mode %s"}\n' "$MODE"
    exit 4
    ;;
esac

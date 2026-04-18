# Meeting-in-Thread Implementation Plan

**Goal**: Treat a meeting as a specialization of `thread` (`thread.metadata.kind = "meeting"`), with both file-upload (Doubao 妙记 batch) and live-stream (Doubao streaming WS) paths. Half-auto task creation from extracted action items. Per-workspace credential storage in `workspace_secret`.

> **Correction (2026-04-19, mid-flight)**: an earlier draft of this plan
> targeted `session`. Migration 053 dropped the session table; the
> conversation primitive today is `thread` (under `channel`). All routes
> moved from `/api/sessions/{id}/meeting/*` → `/api/threads/{id}/meeting/*`,
> and meeting state lives in `thread.metadata` JSONB (no new columns).
> Migration 063 now only adds two indexes on `thread.metadata->>'kind'`.

**Tech stack**: Go 1.26 / Chi / sqlc / pgx — Next.js 16 / React 19 / zustand — Doubao Bigmodel ASR + 妙记 — AES-GCM via existing `workspace_secret` envelope.

**Selected by user 2026-04-19**: A+B 录音模式 / 半自动 task / workspace_secret 凭证。

---

## 1. Data model — single migration, additive only

**File**: `server/migrations/063_meeting_in_session.up.sql` (+`.down.sql`)

```sql
-- Extend session for meeting kind. NOT a new table.
ALTER TABLE session ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'discussion';
-- 'discussion' (existing) | 'meeting' | future kinds
ALTER TABLE session ADD CONSTRAINT chk_session_kind
    CHECK (kind IN ('discussion','meeting'));
CREATE INDEX IF NOT EXISTS idx_session_workspace_kind
    ON session (workspace_id, kind, created_at DESC);

-- Status state-machine extension. Existing values keep working;
-- new ones are exclusively used by meeting kind.
-- (No CHECK constraint on session.status today — see migration 032 — so this
-- needs no schema change beyond documentation in handler/meeting.go.)
-- Lifecycle: planned → recording → transcribing → summarized → completed
--                  ↘ cancelled  (any time)

-- Add a small per-row payload pointer pattern. Audio + transcript live in
-- file_index (avoid bloating session.context JSONB beyond ~16KB).
ALTER TABLE session ADD COLUMN IF NOT EXISTS audio_file_id    UUID REFERENCES file_index(id) ON DELETE SET NULL;
ALTER TABLE session ADD COLUMN IF NOT EXISTS transcript_file_id UUID REFERENCES file_index(id) ON DELETE SET NULL;

-- session.context JSONB shape for kind='meeting':
--   { "kind": "meeting",
--     "agenda": [...],                 -- pre-meeting
--     "briefing": { ... },              -- pre-meeting agent output
--     "transcript_summary_ptr": <uri>,  -- pointer when transcript huge
--     "summary": { sections, decisions, qna },  -- post-meeting妙记 output
--     "action_items": [{id,task,owner,due_date,confidence,task_id?}],
--     "asr_provider": "doubao_streaming"|"doubao_miaoji",
--     "started_at": ts, "ended_at": ts }
```

**Why no `meeting` table**: every column we'd add is meeting-only and nullable when kind != meeting. Reusing `session` keeps participant model, WebSocket Hub, and `/api/sessions/*` routes for free.

---

## 2. Backend — service + handler layout

| File | New/Edit | Responsibility |
|---|---|---|
| `internal/service/asr/doubao.go` | NEW | Doubao adapter. Two surfaces: `BatchSummarize(audioURL) ActionItems` (妙记 submit→poll); `StreamASR(ctx, audioReader) <-chan Segment` (WebSocket to bigmodel ASR) |
| `internal/service/asr/credentials.go` | NEW | Decrypt `workspace_secret` rows (`feishu_app_id`, `feishu_access_token`, `feishu_secret_key`). Cache hot path for ~60s |
| `internal/service/meeting.go` | NEW | Orchestration: `StartMeeting`, `AttachAudio`, `IngestSegment`, `Summarize`, `ExtractActionItems`, `CreateTasksFromAction`. Calls ASR + LLM + task creation; emits Bus events |
| `internal/handler/meeting.go` | NEW | REST handlers (see §4) |
| `internal/realtime/asr_proxy.go` | NEW | Browser WS upgrade → forward 16kHz PCM frames to Doubao streaming ASR; relay back ASR segments via existing Hub |
| `internal/handler/workspace_secret.go` | EDIT | Add convenience PUT for the 3 Feishu keys at once (`PUT /api/workspaces/{id}/secrets/feishu`) |
| `pkg/db/queries/session.sql` | EDIT | Add `UpdateSessionKind`, `UpdateSessionStatus`, `AppendActionItem` (json patch via `jsonb_set`) |
| `pkg/db/queries/file_index.sql` | (no change) | Reuse `CreateFileIndex` for audio + transcript files |

**LLM fallback**: when 妙记 doesn't return a clean `action_items`, call workspace's configured LLM (DashScope qwen3-coder-plus via existing Anthropic-compat proxy) with the transcript + action_item JSON-schema prompt. Reuse existing `service/agent_runner` pathway.

---

## 3. MCP tools — 4 new tools so agents can drive meetings

| Tool | Purpose | Input | Output |
|---|---|---|---|
| `start_meeting` | Create session with kind=meeting + agenda + invitees | `{title, agenda[], invitee_ids[]}` | `{session_id, briefing}` |
| `add_transcript_segment` | Append a transcript segment (used by ASR proxy + manual paste) | `{session_id, speaker, text, ts_start, ts_end}` | `{segment_id}` |
| `summarize_meeting` | Trigger summary + action_item extraction | `{session_id, force?}` | `{summary, action_items[]}` |
| `meeting_advise` | Mid-meeting "what should I ask?" — reads recent N segments, calls LLM | `{session_id, question?}` | `{advice}` |

All gated by `ensureWorkspaceMember` + `session_participant` membership check (new helper).

---

## 4. REST + WebSocket route table

### REST (under existing workspace-membership middleware)

| Method | Path | Handler | Body | Returns | Notes |
|---|---|---|---|---|---|
| POST | `/api/sessions/{id}/meeting/start` | `StartMeeting` | `{agenda?:string[], invitees?:string[]}` | 200 + session | flips `kind=meeting`, status=planned |
| POST | `/api/sessions/{id}/meeting/audio` | `AttachAudio` | multipart `file` | 201 + `{file_index_id}` | uploads to file_index, sets `audio_file_id`, status=transcribing |
| POST | `/api/sessions/{id}/meeting/recording/start` | `StartRecording` | `{sample_rate?:16000}` | 200 + `{ws_url}` | server returns the ASR-proxy WS URL (path `/ws/asr/{id}?token=...`) |
| POST | `/api/sessions/{id}/meeting/recording/stop` | `StopRecording` | – | 200 + `{transcript_file_id}` | flushes WS, persists transcript, status=summarized |
| POST | `/api/sessions/{id}/meeting/summarize` | `SummarizeMeeting` | `{provider?:"miaoji"\|"llm"}` | 202 + `{job_id}` | async; client polls or listens via WS |
| GET | `/api/sessions/{id}/meeting/transcript` | `GetTranscript` | – | 200 + `{segments[]}` | streams from transcript_file_id |
| GET | `/api/sessions/{id}/meeting/action-items` | `ListActionItems` | – | 200 + `{action_items[]}` | each item shows `confidence` + suggested member match |
| POST | `/api/sessions/{id}/meeting/action-items/{itemId}/approve` | `ApproveActionItem` | `{primary_assignee_id}` | 201 + `{task_id}` | the half-auto step: human approves → creates Task on a fresh plan |
| POST | `/api/sessions/{id}/meeting/action-items/{itemId}/reject` | `RejectActionItem` | – | 204 | tombstones the item |
| POST | `/api/sessions/{id}/meeting/briefing` | `GenerateBriefing` | – | 200 + `{briefing}` | optional pre-meeting trigger; reads channel/project history |
| POST | `/api/sessions/{id}/meeting/advise` | `Advise` | `{question?}` | 200 + `{advice}` | mid-meeting QA |

### WebSocket

| Path | Direction | Frame format | Purpose |
|---|---|---|---|
| `/ws/asr/{sessionId}?token=<jwt>` | client → server | binary 16kHz PCM (or webm-opus) | live audio stream |
| `/ws/asr/{sessionId}` | server → client | json `{type:"segment", text, ts_start, ts_end, final}` | ASR results |
| `/ws` (existing Hub) | server → all session participants | json `{type:"meeting.summary_ready"\|"meeting.action_items_ready", session_id, ...}` | post-summary push |
| `/ws` (existing Hub) | server → participants | `{type:"meeting.advice", session_id, advice}` | live thinking output |

### workspace_secret keys (per workspace)

| Key | Purpose | Required for |
|---|---|---|
| `feishu_app_id` | Feishu open APP id | both ASR paths |
| `feishu_access_token` | Doubao API access token | both ASR paths |
| `feishu_secret_key` | Doubao API signing secret | both ASR paths |
| `meeting_llm_api_key` (optional) | LLM fallback for summary/action when 妙记 weak | summary fallback |

PUT shortcut: `PUT /api/workspaces/{id}/secrets/feishu` accepts `{app_id, access_token, secret_key}` and writes 3 rows in one tx.

---

## 5. Frontend — files to add/edit

| File | Action | Purpose |
|---|---|---|
| `apps/web/app/(dashboard)/sessions/[id]/page.tsx` | EDIT | When session.kind == "meeting", render `<MeetingRoom>` instead of generic chat |
| `apps/web/features/meetings/components/MeetingRoom.tsx` | NEW | Top-level layout: Briefing card + RecordButton + TranscriptPanel + ActionItemsPanel |
| `apps/web/features/meetings/components/RecordButton.tsx` | NEW | MediaRecorder → WebSocket to `/ws/asr/{id}` |
| `apps/web/features/meetings/components/TranscriptPanel.tsx` | NEW | Live segments via WS subscription; final segments persisted |
| `apps/web/features/meetings/components/ActionItemsPanel.tsx` | NEW | List of extracted items + Approve/Reject buttons; member dropdown for primary_assignee_id |
| `apps/web/features/meetings/store.ts` | NEW | zustand store: meetingId, transcript[], actionItems[], briefing, summary |
| `apps/web/shared/api/meeting.ts` | NEW | Typed REST client for /api/sessions/{id}/meeting/* |
| `apps/web/app/(dashboard)/workspace-settings/secrets/page.tsx` | EDIT | Add Feishu form (app_id + token + secret) |

---

## 6. Implementation phases

| Phase | Scope | Worktree | Days | Verification |
|---|---|---|---|---|
| **0. Migration + schema** | 063 migration, sqlc regen, workspace_secret PUT shortcut | `.worktrees/meeting-schema` | 0.3 | `make sqlc && make test` green |
| **1. MVP file-upload (B path)** | meeting service + 4 REST handlers (start, attach audio, summarize, list action_items) + 妙记 batch adapter + half-auto approve→task | `.worktrees/meeting-mvp` | 1.2 | curl: upload mp3 → poll → action_items appear → approve one → task created |
| **2. ASR streaming (A path)** | WS proxy + Doubao streaming adapter + RecordButton + TranscriptPanel | `.worktrees/meeting-streaming` | 2.0 | browser: click record → speak → transcript appears in real time |
| **3. Briefing + Advise** | GenerateBriefing handler (channel/project history → LLM), Advise handler, MeetingRoom briefing card + advise button | `.worktrees/meeting-bd` | 1.0 | new meeting: briefing card auto-fills; mid-meeting click "what's next?" returns advice |
| **4. MCP tools** | 4 new MCP tools so agents can drive meetings (not just humans) | `.worktrees/meeting-mcp` | 0.5 | unit tests pass; cloud agent can call `start_meeting` |
| **5. E2E test** | Playwright spec covering full happy path + workspace_secret onboarding | `.worktrees/meeting-e2e` | 0.5 | `make check` green |

**Total ≈ 5.5 days serial.** Phases 1-2 can run in parallel via two Codex/Claude subagents in separate worktrees if you want to compress.

---

## 7. Risks / open issues

| Risk | Mitigation |
|---|---|
| Doubao WebSocket spec drift (last public doc 2025) | Pin a concrete version in `asr/doubao.go`; add e2e test against a recorded sample mp3 to catch regressions |
| Audio file size > 100MB (long meetings) | file_index already supports streaming uploads; chunk to S3-style storage_path; don't load into memory |
| `workspace_secret.value_encrypted` envelope format | Reuse existing `internal/secret/` AES-GCM helpers (audit before relying — flag if missing) |
| Owner-name mismatch in action items | Half-auto means the user sees a member-dropdown next to each item; suggested match shown but not enforced |
| Transcript privacy | Audio + transcript stored in file_index with workspace-scope access; DELETE meeting cascades them via the existing `ON DELETE SET NULL` (audio rows persist for now — issue to track) |
| Browser audio permissions | First-time prompt is fine; UX should fall back to file-upload if denied |

---

## 8. Out of scope for this plan

- Live cursor/presence in the meeting room (not requested)
- Cross-meeting RAG over past summaries (future: ties into MyMemo memory hub)
- Speaker diarization (Doubao 妙记 already does basic; we just surface what they return)
- Calendar integration (no inputs about calendar source yet)
- Meeting recording archival policy / GDPR retention controls (will need a follow-up plan)

---

## 9. Approval gate

**Before I touch code I need confirmation on:**

1. **Phasing**: serial 0→5 over ~5.5 days, OR parallel (you spawn me on phase 1 and Codex on phase 2 simultaneously — both target separate worktrees, merge to main when both green)?
2. **Migration 063**: OK to add `kind` + 2 nullable FK columns to `session` (production-safe, additive only — but it's a top-traffic table)?
3. **Frontend route**: replace `sessions/[id]` rendering when kind=meeting, OR put meetings on a sibling route `/meetings/[id]` so the chat surface is untouched?

回 "**串行/并行 + 改原路由/新路由**"（如 `串行+改原路由`）我就开工 phase 0+1。或者直接 "go" 走推荐路径（串行 + 改原路由 + 5.5 天）。

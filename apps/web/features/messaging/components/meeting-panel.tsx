"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Mic,
  Square,
  Loader2,
  Star,
  StickyNote,
  X,
  AlertTriangle,
  CheckCircle2,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/shared/api";
import type {
  ChannelMeeting,
  MeetingProgressPayload,
  MeetingCompletedPayload,
  MeetingFailedPayload,
} from "@/shared/types";
import { useAuthStore } from "@/features/auth";
import { useWSEvent } from "@/features/realtime";
import {
  LiveAsrClient,
  type LiveAsrUtterance,
} from "@/features/messaging/lib/live-asr-client";

// Live ASR now runs through the server-side Volcengine sauc relay
// instead of the browser's Web Speech API — cross-browser, diarized,
// and not dependent on Google-hosted recognition from mainland China.

/**
 * Channel-scoped meeting panel (migration 076).
 *
 * Slack-style sidebar that drops into the channel view when the user
 * clicks "开始会议". Drives the full lifecycle:
 *
 *   1. No meeting  → topic input + start.
 *   2. recording   → MediaRecorder captures mic, timer ticks, stop
 *                    uploads blob to /api/meetings/:id/audio.
 *   3. processing  → server transcribes via Doubao; polls /meetings/:id
 *                    every 4s until status flips.
 *   4. completed   → renders transcript + summary + notes + highlights.
 *                    Summary view bolds notes/highlight text per the
 *                    "会议总结后加粗显示记录的笔记与重点标记" ask.
 */
export function MeetingPanel({
  channelId,
  initialMeetingId,
  onClose,
}: {
  channelId: string;
  initialMeetingId?: string | null;
  onClose: () => void;
}) {
  const [meeting, setMeeting] = useState<ChannelMeeting | null>(null);
  const [topic, setTopic] = useState("");
  const [starting, setStarting] = useState(false);
  const [recording, setRecording] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [notesDraft, setNotesDraft] = useState("");
  const [savingNotes, setSavingNotes] = useState(false);
  const [highlightDraft, setHighlightDraft] = useState("");
  // Per-segment note composer: when the user clicks the "📝" on a live
  // transcript line, an inline textarea opens under that segment. One
  // at a time — opening a second closes the first.
  const [activeNoteSegId, setActiveNoteSegId] = useState<string | null>(null);
  const [segNoteDraft, setSegNoteDraft] = useState("");

  // Live transcription progress, populated by `meeting:progress` WS
  // events. Cleared when the meeting leaves processing status.
  const [progress, setProgress] = useState<{
    attempt: number;
    elapsedMs: number;
    doubaoStatus?: string;
  } | null>(null);

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<BlobPart[]>([]);
  const streamRef = useRef<MediaStream | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const startTsRef = useRef<number>(0);

  // Live multi-speaker transcript powered by Volcengine sauc bigmodel
  // streaming ASR. Each utterance carries an optional `speakerId` for
  // diarized coloring; null means the server didn't assign one yet.
  type LiveSegment = {
    id: string;
    speaker: string;
    speakerId: number | null;
    text: string;
    interim: boolean;
    ts: number;
  };
  const [liveSegments, setLiveSegments] = useState<LiveSegment[]>([]);
  const [speechSupported, setSpeechSupported] = useState(true);
  const liveAsrRef = useRef<LiveAsrClient | null>(null);
  const currentUser = useAuthStore((s) => s.user);
  const speakerName = currentUser?.name ?? currentUser?.email ?? "我";

  // When opened with an existing meeting id (clicked an inline meeting
  // bubble after it finished), preload the row so the panel goes straight
  // to the summary/transcript view instead of showing the "start a new
  // meeting" screen.
  useEffect(() => {
    if (!initialMeetingId) return;
    let cancelled = false;
    api
      .getChannelMeeting(initialMeetingId)
      .then((m) => {
        if (!cancelled) setMeeting(m);
      })
      .catch((e) => {
        if (!cancelled) toast.error(e instanceof Error ? e.message : "加载会议失败");
      });
    return () => {
      cancelled = true;
    };
  }, [initialMeetingId]);

  // Poll processing meetings every 4s so completion ticks in even
  // without a WS event.
  useEffect(() => {
    if (!meeting || meeting.status !== "processing") return;
    const timer = setInterval(async () => {
      try {
        const next = await api.getChannelMeeting(meeting.id);
        setMeeting(next);
      } catch (e) {
        // Non-fatal — next tick retries.
        console.warn("meeting poll failed", e);
      }
    }, 4000);
    return () => clearInterval(timer);
  }, [meeting?.id, meeting?.status]);

  // Reset progress when meeting changes or leaves processing.
  useEffect(() => {
    if (!meeting || meeting.status !== "processing") {
      setProgress(null);
    }
  }, [meeting?.id, meeting?.status]);

  // WS listeners — Doubao pipeline emits per-poll progress, plus
  // terminal completed/failed. Filter on meeting_id so stray workspace
  // broadcasts don't leak into another open panel.
  const currentMeetingId = meeting?.id ?? null;
  useWSEvent("meeting:progress", (raw) => {
    const p = raw as MeetingProgressPayload;
    if (!currentMeetingId || p.meeting_id !== currentMeetingId) return;
    setProgress({
      attempt: p.attempt ?? 0,
      elapsedMs: p.elapsed_ms ?? 0,
      doubaoStatus: p.doubao_status,
    });
  });
  useWSEvent("meeting:completed", (raw) => {
    const p = raw as MeetingCompletedPayload;
    if (!currentMeetingId || p.meeting_id !== currentMeetingId) return;
    // Cheap refetch so the UI flips instantly without waiting for the
    // 4s poll cycle.
    void api
      .getChannelMeeting(currentMeetingId)
      .then(setMeeting)
      .catch(() => {});
  });
  useWSEvent("meeting:failed", (raw) => {
    const p = raw as MeetingFailedPayload;
    if (!currentMeetingId || p.meeting_id !== currentMeetingId) return;
    void api
      .getChannelMeeting(currentMeetingId)
      .then(setMeeting)
      .catch(() => {});
  });

  // Sync notes draft when the server echoes a new value (e.g. after
  // summary arrives). Don't clobber if the user is actively editing.
  useEffect(() => {
    if (meeting && meeting.notes !== notesDraft && document.activeElement?.tagName !== "TEXTAREA") {
      setNotesDraft(meeting.notes ?? "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meeting?.id, meeting?.updated_at]);

  const stopRecognition = useCallback(() => {
    const client = liveAsrRef.current;
    if (!client) return;
    liveAsrRef.current = null;
    void client.stop().catch(() => {
      /* ignore — connection tear-down errors don't matter once we're done */
    });
  }, []);

  // Build LiveSegment rows from the server's utterance list. Definite
  // utterances are final, non-definite are interim. Speaker labels are
  // derived from speaker_id so the same talker gets a consistent tag.
  const utterancesToSegments = useCallback(
    (utterances: LiveAsrUtterance[]): LiveSegment[] =>
      utterances
        .map((u): LiveSegment | null => {
          const text = u.text?.trim() ?? "";
          if (!text) return null;
          const sid = typeof u.speaker_id === "number" ? u.speaker_id : null;
          const speaker =
            sid == null
              ? speakerName
              : sid === 0
                ? speakerName
                : `说话人 ${sid + 1}`;
          const idBase = `${sid ?? "u"}-${u.start_time}-${u.end_time}`;
          return {
            id: u.definite ? `def-${idBase}` : `int-${idBase}`,
            speaker,
            speakerId: sid,
            text,
            interim: !u.definite,
            ts: Date.now(),
          };
        })
        .filter((s): s is LiveSegment => s !== null),
    [speakerName],
  );

  const startRecognition = useCallback(
    (meetingId: string, topic: string) => {
      if (liveAsrRef.current) return; // already running
      const stream = streamRef.current;
      if (!stream) {
        setSpeechSupported(false);
        return;
      }
      const client = new LiveAsrClient();
      liveAsrRef.current = client;
      client
        .start({
          meetingId,
          topic,
          language: "zh-CN",
          enableSpeaker: true,
          mediaStream: stream,
          onReady: () => {
            setSpeechSupported(true);
          },
          onUtterances: ({ utterances }) => {
            setLiveSegments(utterancesToSegments(utterances));
            // Any utterance is proof the pipeline works — clear any
            // transient "unavailable" state set by an earlier WS error.
            setSpeechSupported(true);
          },
          onError: (msg) => {
            console.warn("live asr error:", msg);
            // Only surface the unavailable banner when there's nothing
            // to show. If utterances have already arrived the user can
            // see the pipeline is working; a spurious ws.onerror during
            // shutdown shouldn't override that signal.
            setLiveSegments((prev) => {
              if (prev.length === 0) setSpeechSupported(false);
              return prev;
            });
          },
          onDone: () => {
            // Final utterances already delivered via onUtterances(final=true).
            // No additional cleanup — stop() happens when recording stops.
          },
        })
        .catch((e) => {
          console.warn("live asr start failed:", e);
          liveAsrRef.current = null;
          setSpeechSupported(false);
        });
    },
    [utterancesToSegments],
  );

  const stopRecorder = useCallback(() => {
    const mr = mediaRecorderRef.current;
    if (mr && mr.state !== "inactive") {
      mr.stop();
    }
    streamRef.current?.getTracks().forEach((t) => t.stop());
    streamRef.current = null;
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    stopRecognition();
    setRecording(false);
  }, [stopRecognition]);

  useEffect(() => {
    return () => {
      // Teardown on unmount — otherwise the mic stays on if the user
      // closes the panel mid-recording.
      stopRecorder();
    };
  }, [stopRecorder]);

  const handleStart = async () => {
    setStarting(true);
    try {
      const created = await api.startChannelMeeting(channelId, { topic });
      setMeeting(created);
      await startRecorder(created);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "开始会议失败");
    } finally {
      setStarting(false);
    }
  };

  const startRecorder = async (m: ChannelMeeting) => {
    if (typeof window === "undefined" || !navigator.mediaDevices?.getUserMedia) {
      toast.error("浏览器不支持麦克风录制");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      streamRef.current = stream;
      const mr = new MediaRecorder(stream, {
        mimeType: pickMimeType(),
      });
      chunksRef.current = [];
      mr.ondataavailable = (ev) => {
        if (ev.data && ev.data.size > 0) {
          chunksRef.current.push(ev.data);
        }
      };
      mr.onstop = () => {
        const blob = new Blob(chunksRef.current, { type: mr.mimeType });
        chunksRef.current = [];
        void finalizeRecording(m, blob);
      };
      mediaRecorderRef.current = mr;
      startTsRef.current = Date.now();
      mr.start(1000);
      setRecording(true);
      setElapsed(0);
      setLiveSegments([]);
      startRecognition(m.id, m.topic);
      timerRef.current = setInterval(() => {
        setElapsed(Math.floor((Date.now() - startTsRef.current) / 1000));
      }, 1000);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "无法访问麦克风");
    }
  };

  const finalizeRecording = async (m: ChannelMeeting, blob: Blob) => {
    setUploading(true);
    try {
      const ext = blob.type.includes("webm") ? "webm" : "m4a";
      const filename = `${m.id}-${Date.now()}.${ext}`;
      const updated = await api.uploadChannelMeetingAudio(m.id, blob, {
        filename,
        durationSec: Math.max(1, Math.floor((Date.now() - startTsRef.current) / 1000)),
      });
      setMeeting(updated);
      toast.success("录音已上传，正在转写");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "上传失败");
    } finally {
      setUploading(false);
    }
  };

  const handleStop = () => {
    stopRecorder();
  };

  const saveNotes = async () => {
    if (!meeting) return;
    setSavingNotes(true);
    try {
      const next = await api.updateChannelMeetingNotes(meeting.id, notesDraft);
      setMeeting(next);
      toast.success("笔记已保存");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSavingNotes(false);
    }
  };

  const addHighlight = async () => {
    if (!meeting || !highlightDraft.trim()) return;
    const next: ChannelMeeting["highlights"] = [
      ...(meeting.highlights ?? []),
      { t: elapsed, text: highlightDraft.trim() },
    ];
    try {
      const updated = await api.updateChannelMeetingHighlights(
        meeting.id,
        next,
      );
      setMeeting(updated);
      setHighlightDraft("");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "添加重点失败");
    }
  };

  // saveSegmentNote appends a structured note entry to meeting.notes:
  //   [MM:SS] speaker: "segment text"
  //     → user note
  // keeps the ordinary notes textarea as the source of truth so nothing
  // else needs to change on the backend. elapsed is captured at save
  // time so the timestamp reflects when the user finished typing,
  // matching the live content they are annotating.
  const saveSegmentNote = async (seg: {
    text: string;
    speaker: string;
  }) => {
    if (!meeting) return;
    const note = segNoteDraft.trim();
    if (!note) return;
    const ts = formatDuration(elapsed);
    const entry = `[${ts}] ${seg.speaker}: "${seg.text.trim()}"\n  → ${note}\n\n`;
    const nextNotes = (notesDraft ?? "") + entry;
    setNotesDraft(nextNotes);
    setSegNoteDraft("");
    setActiveNoteSegId(null);
    setSavingNotes(true);
    try {
      const updated = await api.updateChannelMeetingNotes(meeting.id, nextNotes);
      setMeeting(updated);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存笔记失败");
    } finally {
      setSavingNotes(false);
    }
  };

  // toggleLiveSegmentMark flips the highlight state for a final transcript
  // segment. Clicking an unmarked line adds a highlight stamped at the
  // current elapsed-seconds cursor; clicking a marked line removes it.
  const toggleLiveSegmentMark = async (seg: { text: string }) => {
    if (!meeting) return;
    const text = seg.text.trim();
    if (!text) return;
    const existing = meeting.highlights ?? [];
    const isMarked = existing.some((h) => h.text === text);
    const next: ChannelMeeting["highlights"] = isMarked
      ? existing.filter((h) => h.text !== text)
      : [...existing, { t: elapsed, text }];
    try {
      const updated = await api.updateChannelMeetingHighlights(
        meeting.id,
        next,
      );
      setMeeting(updated);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : (isMarked ? "取消失败" : "标记失败"));
    }
  };

  const removeHighlight = async (idx: number) => {
    if (!meeting) return;
    const next = (meeting.highlights ?? []).filter((_, i) => i !== idx);
    try {
      const updated = await api.updateChannelMeetingHighlights(
        meeting.id,
        next,
      );
      setMeeting(updated);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败");
    }
  };

  return (
    <aside className="w-[360px] shrink-0 border-l border-border flex flex-col bg-background/60">
      <div className="flex items-center justify-between px-3 py-2.5 border-b border-border">
        <div className="flex items-center gap-2">
          <Mic className="h-4 w-4 text-primary" />
          <span className="text-sm font-semibold">会议</span>
          {meeting && (
            <span className="text-[10px] font-mono uppercase text-muted-foreground">
              {statusLabel(meeting.status)}
            </span>
          )}
        </div>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
          aria-label="关闭"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
        {!meeting ? (
          <div className="space-y-2">
            <label className="text-[11px] text-muted-foreground">主题</label>
            <input
              value={topic}
              onChange={(e) => setTopic(e.target.value)}
              placeholder="例如：迭代复盘"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
            />
            <button
              onClick={handleStart}
              disabled={starting}
              className="w-full rounded-md bg-primary text-primary-foreground px-3 py-2 text-sm font-medium disabled:opacity-50"
            >
              {starting ? (
                <Loader2 className="inline h-4 w-4 animate-spin mr-1.5" />
              ) : (
                <Mic className="inline h-4 w-4 mr-1.5" />
              )}
              开始会议
            </button>
            <p className="text-[11px] text-muted-foreground">
              开始后自动录音；结束时转写与 AI 总结由 Doubao 妙记生成。
            </p>
          </div>
        ) : (
          <>
            {/* Recorder controls */}
            {(meeting.status === "recording" || recording) && (
              <div className="rounded-md border border-primary/40 bg-primary/5 px-3 py-2.5 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">
                    {recording ? "录音中" : "已停止录音"}
                  </span>
                  <span className="text-[11px] font-mono text-muted-foreground">
                    {formatDuration(elapsed)}
                  </span>
                </div>
                {recording && (
                  <button
                    onClick={handleStop}
                    disabled={uploading}
                    className="w-full rounded-md border border-border bg-background px-3 py-1.5 text-xs text-destructive hover:bg-destructive/10 transition-colors"
                  >
                    <Square className="inline h-3 w-3 mr-1" />
                    停止并上传
                  </button>
                )}
                {uploading && (
                  <div className="text-[11px] text-muted-foreground flex items-center gap-1.5">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    上传中…
                  </div>
                )}
              </div>
            )}

            {meeting.status === "processing" && (
              <div className="rounded-md border border-border bg-card/60 px-3 py-2.5 space-y-1.5">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  正在生成转写与总结
                </div>
                {progress ? (
                  <p className="text-[11px] text-muted-foreground/80 font-mono">
                    已等待 {Math.round((progress.elapsedMs ?? 0) / 1000)}s
                    {progress.doubaoStatus
                      ? ` · ${progress.doubaoStatus}`
                      : ""}
                    {" · #"}{progress.attempt ?? 0}
                  </p>
                ) : (
                  <p className="text-[11px] text-muted-foreground/70">
                    视频时长而定，通常 1-3 分钟。
                  </p>
                )}
              </div>
            )}

            {/* Live single-speaker transcript — only visible while
                recording. After stop, the server-side (Doubao) diarized
                transcript takes over via <TranscriptView /> below. */}
            {recording && (
              <div className="space-y-1.5">
                <div className="flex items-center gap-2 text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                  <span>实时转写 · 说话人日志</span>
                  {!speechSupported && liveSegments.length === 0 && (
                    <span className="text-destructive normal-case font-sans">
                      实时转写暂不可用
                    </span>
                  )}
                </div>
                {speechSupported && liveSegments.length === 0 && (
                  <div className="text-[11px] text-muted-foreground/70 px-2 py-2 border border-dashed border-border rounded">
                    开始说话，转写会实时显示在这里。
                  </div>
                )}
                {liveSegments.length > 0 && (
                  <ul className="space-y-1 max-h-60 overflow-y-auto rounded bg-muted/20 p-2">
                    {liveSegments.map((seg) => {
                      const marked =
                        !seg.interim &&
                        (meeting?.highlights ?? []).some(
                          (h) => h.text === seg.text.trim(),
                        );
                      const noteOpen = activeNoteSegId === seg.id;
                      return (
                        <li
                          key={seg.id}
                          className={`group/seg text-[11px] leading-snug rounded-md border transition-colors ${
                            marked
                              ? "border-amber-400 bg-amber-400/10 shadow-[0_0_0_1px_rgb(251_191_36_/_0.5)]"
                              : "border-transparent"
                          }`}
                        >
                          <div
                            role={!seg.interim ? "button" : undefined}
                            tabIndex={!seg.interim ? 0 : undefined}
                            onClick={() => {
                              if (seg.interim) return;
                              void toggleLiveSegmentMark(seg);
                            }}
                            onKeyDown={(e) => {
                              if (seg.interim) return;
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                void toggleLiveSegmentMark(seg);
                              }
                            }}
                            aria-pressed={!seg.interim ? marked : undefined}
                            aria-label={
                              seg.interim
                                ? undefined
                                : marked
                                  ? "取消重点标记"
                                  : "标记为重点"
                            }
                            title={
                              seg.interim
                                ? undefined
                                : marked
                                  ? "点击取消重点"
                                  : "点击标记为重点"
                            }
                            className={`flex items-start gap-1.5 px-1.5 py-0.5 rounded-md ${
                              seg.interim
                                ? ""
                                : "cursor-pointer hover:bg-muted/40 focus:outline-none focus:ring-1 focus:ring-amber-400"
                            }`}
                          >
                            <span className="flex-1">
                              <span className={`font-mono mr-1 ${speakerColor(seg.speakerId)}`}>
                                {seg.speaker}:
                              </span>
                              <span className={seg.interim ? "text-muted-foreground" : ""}>
                                {seg.text}
                              </span>
                            </span>
                            {marked && (
                              <Star
                                className="h-3 w-3 shrink-0 mt-0.5 text-amber-400"
                                fill="currentColor"
                                aria-hidden="true"
                              />
                            )}
                            {!seg.interim && (
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  setActiveNoteSegId(
                                    noteOpen ? null : seg.id,
                                  );
                                  setSegNoteDraft("");
                                }}
                                aria-label={noteOpen ? "取消笔记" : "添加笔记"}
                                title={noteOpen ? "取消笔记" : "添加笔记"}
                                className={`shrink-0 mt-0.5 transition-opacity ${
                                  noteOpen
                                    ? "text-primary opacity-100"
                                    : "text-muted-foreground opacity-0 group-hover/seg:opacity-100 hover:text-primary"
                                }`}
                              >
                                <StickyNote className="h-3 w-3" />
                              </button>
                            )}
                          </div>
                          {noteOpen && (
                            <div
                              className="mt-1 ml-2 flex items-start gap-1.5"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <textarea
                                value={segNoteDraft}
                                onChange={(e) => setSegNoteDraft(e.target.value)}
                                placeholder="针对这一句的笔记…"
                                rows={2}
                                autoFocus
                                onClick={(e) => e.stopPropagation()}
                                onKeyDown={(e) => {
                                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                                    e.preventDefault();
                                    void saveSegmentNote(seg);
                                  }
                                  if (e.key === "Escape") {
                                    e.preventDefault();
                                    setActiveNoteSegId(null);
                                    setSegNoteDraft("");
                                  }
                                }}
                                className="flex-1 rounded-md border border-border bg-background px-2 py-1 text-[11px]"
                              />
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  void saveSegmentNote(seg);
                                }}
                                disabled={!segNoteDraft.trim() || savingNotes}
                                className="rounded-md border border-border bg-primary text-primary-foreground px-2 py-1 text-[11px] disabled:opacity-50"
                              >
                                保存
                              </button>
                            </div>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
                {!speechSupported && liveSegments.length === 0 && (
                  <p className="text-[10px] text-muted-foreground/70">
                    实时转写当前不可用（服务器未配置或连接失败）。录音结束后仍会通过服务端完整转写生成最终日志。
                  </p>
                )}
              </div>
            )}

            {meeting.status === "failed" && (
              <div className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2.5 space-y-1">
                <div className="flex items-center gap-2 text-xs text-destructive">
                  <AlertTriangle className="h-3.5 w-3.5" />
                  转写失败
                </div>
                {meeting.failure_reason && (
                  <p className="text-[11px] text-muted-foreground whitespace-pre-wrap">
                    {meeting.failure_reason}
                  </p>
                )}
              </div>
            )}

            {/* Transcript */}
            {meeting.transcript && (
              <div className="space-y-1.5">
                <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                  转写 · 说话人日志
                </div>
                <TranscriptView transcript={meeting.transcript} />
              </div>
            )}

            {/* Summary with bold notes + highlights */}
            {meeting.status === "completed" && (
              <SummarySection meeting={meeting} />
            )}

            {/* Notes */}
            <div className="space-y-1.5">
              <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                笔记
              </div>
              <textarea
                value={notesDraft}
                onChange={(e) => setNotesDraft(e.target.value)}
                onBlur={saveNotes}
                placeholder="记录关键决策、待办、疑问…"
                rows={5}
                className="w-full rounded-md border border-border bg-background px-2.5 py-2 text-xs"
              />
              {savingNotes && (
                <div className="text-[10px] text-muted-foreground">保存中…</div>
              )}
            </div>

            {/* Highlights */}
            <div className="space-y-1.5">
              <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                重点标记
              </div>
              <div className="flex items-center gap-1.5">
                <input
                  value={highlightDraft}
                  onChange={(e) => setHighlightDraft(e.target.value)}
                  placeholder="例如：决定下周上线 v1.0"
                  className="flex-1 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs"
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      void addHighlight();
                    }
                  }}
                />
                <button
                  onClick={addHighlight}
                  disabled={!highlightDraft.trim()}
                  className="rounded-md border border-border bg-background px-2 py-1.5 text-xs hover:bg-accent disabled:opacity-50"
                >
                  <Star className="h-3 w-3" />
                </button>
              </div>
              {(meeting.highlights ?? []).length > 0 && (
                <ul className="space-y-1">
                  {meeting.highlights.map((h, i) => (
                    <li
                      key={i}
                      className="flex items-start gap-1.5 text-[11px] leading-snug"
                    >
                      <Star className="h-3 w-3 text-primary shrink-0 mt-0.5" />
                      <span className="flex-1">
                        {typeof h.t === "number" && (
                          <span className="text-muted-foreground font-mono mr-1">
                            {formatDuration(h.t)}
                          </span>
                        )}
                        {h.text}
                      </span>
                      <button
                        onClick={() => removeHighlight(i)}
                        className="text-muted-foreground hover:text-destructive shrink-0"
                        aria-label="删除"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </>
        )}
      </div>
    </aside>
  );
}

function statusLabel(s: ChannelMeeting["status"]): string {
  return {
    recording: "录音中",
    processing: "转写中",
    completed: "已完成",
    failed: "失败",
  }[s];
}

function formatDuration(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

// speakerColor maps a diarized speaker_id to a stable Tailwind text
// color. Speaker 0 (current user) reuses the primary tone so the
// "self" voice stays visually consistent with other UI; others cycle
// through an accessible palette.
function speakerColor(speakerId: number | null): string {
  if (speakerId == null || speakerId === 0) return "text-primary";
  const palette = [
    "text-sky-500",
    "text-emerald-500",
    "text-rose-500",
    "text-violet-500",
    "text-amber-500",
    "text-cyan-500",
    "text-fuchsia-500",
  ];
  return palette[(speakerId - 1) % palette.length] ?? "text-primary";
}

function pickMimeType(): string {
  // Prefer opus in webm — broadest browser support + compact size.
  const candidates = [
    "audio/webm;codecs=opus",
    "audio/webm",
    "audio/mp4",
  ];
  for (const c of candidates) {
    if (
      typeof MediaRecorder !== "undefined" &&
      typeof MediaRecorder.isTypeSupported === "function" &&
      MediaRecorder.isTypeSupported(c)
    ) {
      return c;
    }
  }
  return "audio/webm";
}

/* -------------- Sub-views -------------- */

function TranscriptView({ transcript }: { transcript: Record<string, unknown> }) {
  // Doubao memo "Transcription" block — shape is typically {utterances:
  // [{speaker_id, text, start_time, end_time}, ...]} but shapes vary
  // across versions. Duck-type read with fallbacks so we render
  // something even when the API changes names slightly.
  const utterancesRaw =
    (transcript.utterances as unknown) ??
    (transcript.Utterances as unknown) ??
    (transcript.sentences as unknown) ??
    [];
  const utterances = Array.isArray(utterancesRaw)
    ? (utterancesRaw as Array<Record<string, unknown>>)
    : [];
  if (utterances.length === 0) {
    return (
      <pre className="text-[10px] leading-snug text-muted-foreground font-mono whitespace-pre-wrap break-words max-h-60 overflow-y-auto rounded bg-muted/40 p-2">
        {JSON.stringify(transcript, null, 2)}
      </pre>
    );
  }
  return (
    <ul className="space-y-1 max-h-60 overflow-y-auto rounded bg-muted/20 p-2">
      {utterances.map((u, i) => {
        // Doubao memo returns speaker as {id, name, type}; the
        // streaming sauc shape returns a flat speaker_id number. Be
        // tolerant to both — never render the object directly or
        // React throws "Objects are not valid as a React child".
        const rawSpeaker = u.speaker ?? u.Speaker;
        let speakerLabel = "";
        let speakerIdNum: number | null = null;
        if (rawSpeaker && typeof rawSpeaker === "object") {
          const s = rawSpeaker as Record<string, unknown>;
          speakerLabel =
            (typeof s.name === "string" && s.name) ||
            (typeof s.Name === "string" && s.Name) ||
            "";
          const idRaw = s.id ?? s.Id ?? s.ID;
          if (typeof idRaw === "string") {
            const n = Number(idRaw);
            if (Number.isFinite(n)) speakerIdNum = n;
          } else if (typeof idRaw === "number") {
            speakerIdNum = idRaw;
          }
        } else if (typeof rawSpeaker === "string") {
          speakerLabel = rawSpeaker;
        } else if (typeof u.speaker_id === "number") {
          speakerIdNum = u.speaker_id as number;
        } else if (typeof u.speaker_id === "string") {
          const n = Number(u.speaker_id);
          if (Number.isFinite(n)) speakerIdNum = n;
          else speakerLabel = u.speaker_id;
        }
        if (!speakerLabel && speakerIdNum != null) {
          speakerLabel = `说话人 ${speakerIdNum}`;
        }
        if (!speakerLabel) speakerLabel = "S?";

        const text =
          (u.text as string) ??
          (u.Text as string) ??
          (u.content as string) ??
          (u.Content as string) ??
          (u.sentence as string) ??
          "";
        return (
          <li key={i} className="text-[11px] leading-snug">
            <span className={`font-mono mr-1 ${speakerColor(speakerIdNum)}`}>
              {speakerLabel}:
            </span>
            <span>{text}</span>
          </li>
        );
      })}
    </ul>
  );
}

function SummarySection({ meeting }: { meeting: ChannelMeeting }) {
  const summary = meeting.summary ?? {};
  // The server flattens Doubao's per-section file URLs into these
  // flat keys at transcription completion time. We fall back to the
  // legacy camel-case shapes in case any old row still carries them.
  const title =
    pickString(summary, ["title", "Title"]) ||
    pickString(summary.Summary ?? {}, ["title", "Title"]);
  const summaryText =
    pickString(summary, ["summary", "Summary", "summaryText", "SummaryText"]) ||
    pickString(summary.Summary ?? {}, ["content", "text", "paragraph"]) ||
    "";
  const chapters = pickArray(summary, ["Chapters", "chapters", "chapter_summary"]);
  const todos = pickArray(summary, ["Todos", "TodoList", "todo_list", "todos"]);
  const qa = pickArray(summary, ["QA", "qa", "question_answer", "QuestionAnswer"]);

  return (
    <div className="rounded-md border border-border bg-card/60 p-2.5 space-y-2">
      <div className="flex items-center gap-1.5 text-[11px] font-medium text-foreground">
        <CheckCircle2 className="h-3.5 w-3.5 text-[#4ade80]" />
        会议总结
      </div>

      {title && (
        <p className="text-[11px] font-semibold leading-snug">
          {title}
        </p>
      )}

      {summaryText && (
        <p className="text-[11px] leading-relaxed whitespace-pre-wrap">
          {summaryText}
        </p>
      )}

      {/* Notes + highlights bolded per the product spec so the
          user-curated bits stand out against auto-generated summary
          text. */}
      {meeting.notes?.trim() && (
        <div className="space-y-1">
          <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
            笔记
          </div>
          <p className="text-[11px] leading-relaxed font-semibold whitespace-pre-wrap">
            {meeting.notes}
          </p>
        </div>
      )}
      {(meeting.highlights ?? []).length > 0 && (
        <div className="space-y-1">
          <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
            重点
          </div>
          <ul className="space-y-0.5">
            {meeting.highlights.map((h, i) => (
              <li key={i} className="text-[11px] font-bold">
                · {h.text}
              </li>
            ))}
          </ul>
        </div>
      )}

      {chapters.length > 0 && (
        <div className="space-y-1">
          <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
            章节
          </div>
          <ul className="space-y-0.5">
            {chapters.map((c, i) => (
              <li key={i} className="text-[11px]">
                · {pickString(c, ["title", "Title", "content", "Content"])}
              </li>
            ))}
          </ul>
        </div>
      )}

      {todos.length > 0 && (
        <div className="space-y-1">
          <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
            待办
          </div>
          <ul className="space-y-0.5">
            {todos.map((t, i) => (
              <li key={i} className="text-[11px]">
                · {pickString(t, ["content", "Content", "text", "Text"])}
              </li>
            ))}
          </ul>
        </div>
      )}

      {qa.length > 0 && (
        <div className="space-y-1">
          <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
            问答
          </div>
          <ul className="space-y-1">
            {qa.map((q, i) => {
              const question = pickString(q, ["question", "Question", "q", "Q"]);
              const answer = pickString(q, ["answer", "Answer", "a", "A"]);
              if (!question && !answer) return null;
              return (
                <li key={i} className="text-[11px] leading-relaxed">
                  {question && (
                    <div className="font-semibold">Q: {question}</div>
                  )}
                  {answer && (
                    <div className="text-muted-foreground">A: {answer}</div>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}

function pickString(obj: unknown, keys: string[]): string {
  if (!obj || typeof obj !== "object") return "";
  for (const k of keys) {
    const v = (obj as Record<string, unknown>)[k];
    if (typeof v === "string" && v.trim()) return v;
  }
  return "";
}

function pickArray(
  obj: unknown,
  keys: string[],
): Array<Record<string, unknown>> {
  if (!obj || typeof obj !== "object") return [];
  for (const k of keys) {
    const v = (obj as Record<string, unknown>)[k];
    if (Array.isArray(v)) return v as Array<Record<string, unknown>>;
  }
  return [];
}

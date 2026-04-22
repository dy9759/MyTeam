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
import type { ChannelMeeting } from "@/shared/types";
import { useAuthStore } from "@/features/auth";

// Minimal local types for the Web Speech API — the browser's built-in
// names aren't exposed in TypeScript's default lib. Only the surface we
// actually read is typed; anything else stays unknown.
interface WebSpeechResultAlternative {
  transcript?: string;
}
interface WebSpeechResult {
  isFinal: boolean;
  0?: WebSpeechResultAlternative;
  [index: number]: WebSpeechResultAlternative | undefined;
}
interface WebSpeechResultList {
  length: number;
  [index: number]: WebSpeechResult | undefined;
}
interface WebSpeechEvent {
  resultIndex: number;
  results: WebSpeechResultList;
}
interface WebSpeechErrorEvent {
  error: string;
}
interface WebSpeechRecognition {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onresult: ((ev: WebSpeechEvent) => void) | null;
  onerror: ((ev: WebSpeechErrorEvent) => void) | null;
  onend: (() => void) | null;
  start: () => void;
  stop: () => void;
}
type WebSpeechCtor = new () => WebSpeechRecognition;

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

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<BlobPart[]>([]);
  const streamRef = useRef<MediaStream | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const startTsRef = useRef<number>(0);

  // Live single-speaker transcript powered by the browser's SpeechRecognition
  // API. Runs alongside the MediaRecorder so the user sees running text
  // while speaking. Doubao's server-side multi-speaker transcript replaces
  // this view after processing completes.
  type LiveSegment = { id: string; speaker: string; text: string; interim: boolean; ts: number };
  const [liveSegments, setLiveSegments] = useState<LiveSegment[]>([]);
  const [speechSupported, setSpeechSupported] = useState(true);
  const recognitionRef = useRef<WebSpeechRecognition | null>(null);
  // Consecutive-failure backoff: if start() throws or onend fires again
  // within 1s of last start, increment. At ≥ 3 failures inside a 1s
  // window, stop restarting + flip speechSupported=false so the UI
  // fallback message renders.
  const failureCountRef = useRef(0);
  const lastStartTsRef = useRef(0);
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

  // Sync notes draft when the server echoes a new value (e.g. after
  // summary arrives). Don't clobber if the user is actively editing.
  useEffect(() => {
    if (meeting && meeting.notes !== notesDraft && document.activeElement?.tagName !== "TEXTAREA") {
      setNotesDraft(meeting.notes ?? "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meeting?.id, meeting?.updated_at]);

  const stopRecognition = useCallback(() => {
    const rec = recognitionRef.current;
    if (!rec) return;
    try {
      rec.onresult = null;
      rec.onerror = null;
      rec.onend = null;
      rec.stop();
    } catch {
      /* ignore */
    }
    recognitionRef.current = null;
  }, []);

  const startRecognition = useCallback(() => {
    if (typeof window === "undefined") return;
    const Ctor =
      (window as unknown as { SpeechRecognition?: WebSpeechCtor }).SpeechRecognition ??
      (window as unknown as { webkitSpeechRecognition?: WebSpeechCtor }).webkitSpeechRecognition;
    if (!Ctor) {
      setSpeechSupported(false);
      return;
    }
    const rec = new Ctor();
    rec.lang = "zh-CN";
    rec.continuous = true;
    rec.interimResults = true;
    rec.onresult = (ev: WebSpeechEvent) => {
      setLiveSegments((prev) => {
        // Drop any trailing interim segment; we'll re-append a fresh one.
        const kept = prev.filter((s) => !s.interim);
        const updates: LiveSegment[] = [];
        for (let i = ev.resultIndex; i < ev.results.length; i++) {
          const r = ev.results[i];
          if (!r) continue;
          const text = r[0]?.transcript?.trim() ?? "";
          if (!text) continue;
          updates.push({
            id: `${Date.now()}-${i}`,
            speaker: speakerName,
            text,
            interim: !r.isFinal,
            ts: Date.now(),
          });
        }
        return [...kept, ...updates];
      });
    };
    rec.onerror = (ev: WebSpeechErrorEvent) => {
      // Fatal codes — user denied mic, service unavailable, or the
      // audio device is gone. Detach onend + null the ref BEFORE
      // returning so the reference-equality guard in onend can't
      // re-enter and tight-loop rec.start().
      if (
        ev.error === "not-allowed" ||
        ev.error === "service-not-allowed" ||
        ev.error === "audio-capture"
      ) {
        rec.onend = null;
        if (recognitionRef.current === rec) {
          recognitionRef.current = null;
        }
        setSpeechSupported(false);
      }
    };
    rec.onend = () => {
      // Auto-restart while the MediaRecorder is still active — the Web
      // Speech API stops itself after silence or long recognition spans
      // and we want the live feed to keep ticking.
      if (recognitionRef.current !== rec || !streamRef.current) return;
      // Consecutive-failure backoff: if onend fires within 1s of the
      // last start, count it as a failure. ≥ 3 inside a 1s window →
      // stop and surface the unsupported fallback.
      const now = Date.now();
      if (now - lastStartTsRef.current < 1000) {
        failureCountRef.current += 1;
      } else {
        failureCountRef.current = 0;
      }
      if (failureCountRef.current >= 3) {
        rec.onend = null;
        recognitionRef.current = null;
        setSpeechSupported(false);
        return;
      }
      try {
        lastStartTsRef.current = Date.now();
        rec.start();
      } catch {
        failureCountRef.current += 1;
        if (failureCountRef.current >= 3) {
          rec.onend = null;
          recognitionRef.current = null;
          setSpeechSupported(false);
        }
      }
    };
    try {
      failureCountRef.current = 0;
      lastStartTsRef.current = Date.now();
      rec.start();
      recognitionRef.current = rec;
    } catch {
      setSpeechSupported(false);
    }
  }, [speakerName]);

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
      startRecognition();
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

  // markLiveSegment captures a final transcript segment as a highlight
  // without the user having to re-type it. Stamps the elapsed-seconds
  // cursor at click time so the timeline entry lands at the moment the
  // user decided the line was important. De-dupes on exact text to keep
  // accidental double-clicks from stacking.
  const markLiveSegment = async (seg: { text: string }) => {
    if (!meeting) return;
    const text = seg.text.trim();
    if (!text) return;
    const existing = meeting.highlights ?? [];
    if (existing.some((h) => h.text === text)) return;
    const next: ChannelMeeting["highlights"] = [
      ...existing,
      { t: elapsed, text },
    ];
    try {
      const updated = await api.updateChannelMeetingHighlights(
        meeting.id,
        next,
      );
      setMeeting(updated);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "标记失败");
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
                <p className="text-[11px] text-muted-foreground/70">
                  视频时长而定，通常 1-3 分钟。
                </p>
              </div>
            )}

            {/* Live single-speaker transcript — only visible while
                recording. After stop, the server-side (Doubao) diarized
                transcript takes over via <TranscriptView /> below. */}
            {recording && (
              <div className="space-y-1.5">
                <div className="flex items-center gap-2 text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                  <span>实时转写 · 说话人日志</span>
                  {!speechSupported && (
                    <span className="text-destructive normal-case font-sans">
                      浏览器不支持实时转写
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
                          className="group/seg text-[11px] leading-snug"
                        >
                          <div className="flex items-start gap-1.5">
                            <span className="flex-1">
                              <span className="text-primary font-mono mr-1">
                                {seg.speaker}:
                              </span>
                              <span className={seg.interim ? "text-muted-foreground" : ""}>
                                {seg.text}
                              </span>
                            </span>
                            {!seg.interim && (
                              <>
                                <button
                                  type="button"
                                  onClick={() => {
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
                                <button
                                  type="button"
                                  onClick={() => markLiveSegment(seg)}
                                  disabled={marked}
                                  aria-label={marked ? "已标记重点" : "标记为重点"}
                                  title={marked ? "已标记重点" : "标记为重点"}
                                  className={`shrink-0 mt-0.5 transition-opacity ${
                                    marked
                                      ? "text-primary opacity-100"
                                      : "text-muted-foreground opacity-0 group-hover/seg:opacity-100 hover:text-primary"
                                  }`}
                                >
                                  <Star
                                    className="h-3 w-3"
                                    fill={marked ? "currentColor" : "none"}
                                  />
                                </button>
                              </>
                            )}
                          </div>
                          {noteOpen && (
                            <div className="mt-1 ml-2 flex items-start gap-1.5">
                              <textarea
                                value={segNoteDraft}
                                onChange={(e) => setSegNoteDraft(e.target.value)}
                                placeholder="针对这一句的笔记…"
                                rows={2}
                                autoFocus
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
                                onClick={() => void saveSegmentNote(seg)}
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
                {!speechSupported && (
                  <p className="text-[10px] text-muted-foreground/70">
                    当前浏览器未提供本地语音识别。录音结束后仍会通过服务端转写生成完整日志。
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
        const speaker =
          (u.speaker_id as string) ??
          (u.speaker as string) ??
          (u.Speaker as string) ??
          "S?";
        const text =
          (u.text as string) ??
          (u.Text as string) ??
          (u.sentence as string) ??
          "";
        return (
          <li key={i} className="text-[11px] leading-snug">
            <span className="text-muted-foreground font-mono mr-1">
              {speaker}:
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
  // Pull best-effort summary text. Doubao memo returns a Summary
  // object + per-section result URLs; when the server consolidated
  // them we may get {summary: "..."} or {Summary: {content: "..."}}.
  const summaryText =
    pickString(summary, ["summary", "Summary", "summaryText", "SummaryText"]) ||
    pickString(summary.Summary ?? {}, ["content", "text"]) ||
    "";
  const chapters = pickArray(summary, ["Chapters", "chapters"]);
  const todos = pickArray(summary, ["Todos", "TodoList", "todo_list"]);

  return (
    <div className="rounded-md border border-border bg-card/60 p-2.5 space-y-2">
      <div className="flex items-center gap-1.5 text-[11px] font-medium text-foreground">
        <CheckCircle2 className="h-3.5 w-3.5 text-[#4ade80]" />
        会议总结
      </div>

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

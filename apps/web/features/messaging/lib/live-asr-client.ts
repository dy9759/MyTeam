// Live ASR client — orchestrates mic capture, AudioWorklet PCM
// conversion, and a WebSocket relay to the backend /api/asr/stream
// handler which bridges to Volcengine sauc bigmodel_async.
//
// Design notes:
//   - A 16kHz AudioContext lets the browser resample the native mic
//     stream (usually 48kHz) for free via the WebAudio graph, so the
//     worklet only handles float→int16 conversion and chunking.
//   - The worklet ships 100ms buffers; matches the Doubao recommended
//     send cadence for the bigmodel_async endpoint (200ms works too,
//     100ms keeps first-char latency lower).
//   - Token travels as a ?token= query param because browsers cannot
//     set Authorization headers on native WebSocket constructors.

export interface LiveAsrUtterance {
  text: string;
  start_time: number;
  end_time: number;
  definite: boolean;
  speaker_id?: number;
}

export interface LiveAsrHandlers {
  onReady?: (logId: string) => void;
  onUtterances?: (payload: {
    text: string;
    utterances: LiveAsrUtterance[];
    final: boolean;
  }) => void;
  onError?: (message: string) => void;
  onDone?: () => void;
}

export interface LiveAsrStartOptions extends LiveAsrHandlers {
  meetingId: string;
  topic?: string;
  language?: string; // "zh-CN" / "en-US" / empty for auto
  enableSpeaker?: boolean;
  hotWords?: string[];
  // Optional pre-acquired MediaStream. If omitted the client requests
  // its own via getUserMedia. Passing one in lets the caller share the
  // mic with MediaRecorder and still get live ASR.
  mediaStream?: MediaStream;
}

// Resolve the WebSocket host base (scheme://host[:port], no path) that
// this client appends its own /api/asr/stream route onto.
//
// NEXT_PUBLIC_WS_URL points at the main realtime socket (e.g.
// ws://localhost:8080/ws) so we cannot treat it as a naked base — strip
// the trailing path segment. NEXT_PUBLIC_API_URL is an HTTP base and
// maps cleanly to ws:// by scheme swap.
function resolveWsBase(): string {
  const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? "";
  if (apiUrl) {
    try {
      const u = new URL(apiUrl);
      u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
      return `${u.protocol}//${u.host}`;
    } catch {
      /* fall through */
    }
  }
  const wsUrl = process.env.NEXT_PUBLIC_WS_URL ?? "";
  if (wsUrl) {
    try {
      const u = new URL(wsUrl);
      return `${u.protocol}//${u.host}`;
    } catch {
      /* fall through */
    }
  }
  if (typeof window !== "undefined") {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${window.location.host}`;
  }
  return "";
}

export class LiveAsrClient {
  private ws: WebSocket | null = null;
  private ctx: AudioContext | null = null;
  private node: AudioWorkletNode | null = null;
  private source: MediaStreamAudioSourceNode | null = null;
  private ownedStream: MediaStream | null = null;
  private handlers: LiveAsrHandlers = {};
  private started = false;
  private closed = false;

  async start(opts: LiveAsrStartOptions): Promise<void> {
    if (this.started) throw new Error("LiveAsrClient already started");
    this.started = true;
    this.handlers = {
      onReady: opts.onReady,
      onUtterances: opts.onUtterances,
      onError: opts.onError,
      onDone: opts.onDone,
    };

    const token =
      typeof window !== "undefined"
        ? window.localStorage.getItem("myteam_token")
        : null;
    if (!token) {
      throw new Error("auth token missing; log in before starting live ASR");
    }

    const base = resolveWsBase();
    if (!base) throw new Error("cannot resolve WS base url");
    const url = `${base}/api/asr/stream?token=${encodeURIComponent(token)}`;

    // Acquire mic + audio graph first so we can fail fast before
    // committing the WebSocket.
    const stream =
      opts.mediaStream ??
      (await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: 1,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      }));
    if (!opts.mediaStream) this.ownedStream = stream;

    const AudioContextCtor: typeof AudioContext =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext: typeof AudioContext })
        .webkitAudioContext;
    this.ctx = new AudioContextCtor({ sampleRate: 16000 });
    await this.ctx.audioWorklet.addModule("/asr-pcm-worklet.js");

    this.source = this.ctx.createMediaStreamSource(stream);
    this.node = new AudioWorkletNode(this.ctx, "pcm-chunker");
    this.source.connect(this.node);
    // Do NOT connect node to destination — we don't want mic echo.

    this.node.port.onmessage = (ev) => {
      const msg = ev.data as { type: string; pcm?: ArrayBuffer };
      if (msg.type !== "pcm" || !msg.pcm) return;
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(msg.pcm);
      }
    };

    // Now wire up the WS and send the config frame on open.
    this.ws = new WebSocket(url);
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = () => {
      this.ws?.send(
        JSON.stringify({
          type: "config",
          meeting_id: opts.meetingId,
          topic: opts.topic ?? "",
          language: opts.language ?? "",
          enable_speaker: opts.enableSpeaker ?? false,
          hot_words: opts.hotWords ?? [],
        }),
      );
    };
    this.ws.onmessage = (ev) => {
      if (typeof ev.data !== "string") return; // ignore any unexpected binary
      try {
        const msg = JSON.parse(ev.data) as {
          type: string;
          log_id?: string;
          text?: string;
          utterances?: LiveAsrUtterance[];
          final?: boolean;
          message?: string;
        };
        if (msg.type === "ready") {
          this.handlers.onReady?.(msg.log_id ?? "");
        } else if (msg.type === "utterances") {
          this.handlers.onUtterances?.({
            text: msg.text ?? "",
            utterances: msg.utterances ?? [],
            final: Boolean(msg.final),
          });
        } else if (msg.type === "error") {
          this.handlers.onError?.(msg.message ?? "unknown error");
        } else if (msg.type === "done") {
          this.handlers.onDone?.();
        }
      } catch {
        // Non-JSON frame — drop silently.
      }
    };
    this.ws.onerror = () => {
      this.handlers.onError?.("websocket error");
    };
    this.ws.onclose = () => {
      // If we haven't received "done" yet, surface a neutral signal.
      if (!this.closed) this.handlers.onDone?.();
    };
  }

  /** Signal end-of-audio and tear down mic + WS. Idempotent. */
  async stop(): Promise<void> {
    if (this.closed) return;
    this.closed = true;

    // 1. Tell the worklet to flush any partial buffer.
    this.node?.port.postMessage({ type: "stop" });
    // 2. Let the server know no more audio is coming.
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      try {
        this.ws.send(JSON.stringify({ type: "end" }));
      } catch {
        /* ignore */
      }
    }
    // 3. Give the backend a short window to emit terminal events, then
    // disconnect. 3s is plenty given the reader goroutine drains fast.
    await new Promise((r) => setTimeout(r, 300));

    try {
      this.source?.disconnect();
    } catch {
      /* ignore */
    }
    try {
      this.node?.disconnect();
    } catch {
      /* ignore */
    }
    try {
      await this.ctx?.close();
    } catch {
      /* ignore */
    }
    this.ownedStream?.getTracks().forEach((t) => t.stop());
    this.ownedStream = null;
    this.ws?.close();
  }
}

// AudioWorkletProcessor that converts the live mic Float32 stream into
// 16kHz / 16-bit little-endian PCM chunks, batched at ~100ms.
//
// Assumptions:
//   - The host AudioContext was created with sampleRate=16000 so we
//     don't need in-processor resampling (Chrome/Firefox/Safari honor
//     the ctor option).
//   - Chunk size is 1600 samples → 3200 bytes per message → ~100ms of
//     audio per WS frame, matching the Volcengine sauc recommendation.
//
// The processor only emits buffers; all network IO happens on the main
// thread via port.onmessage.

const TARGET_SAMPLE_RATE = 16000;
const CHUNK_SAMPLES = 1600; // ~100ms

class PcmChunkerProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buffer = new Float32Array(CHUNK_SAMPLES);
    this.filled = 0;
    this.active = true;
    this.port.onmessage = (ev) => {
      if (ev.data && ev.data.type === "stop") {
        this.active = false;
        this.flush(true);
      }
    };
  }

  process(inputs) {
    if (!this.active) return false; // unregister processor
    const input = inputs[0];
    if (!input || input.length === 0) return true;
    const channel = input[0];
    if (!channel) return true;

    let srcIndex = 0;
    while (srcIndex < channel.length) {
      const remaining = CHUNK_SAMPLES - this.filled;
      const copy = Math.min(remaining, channel.length - srcIndex);
      this.buffer.set(channel.subarray(srcIndex, srcIndex + copy), this.filled);
      this.filled += copy;
      srcIndex += copy;
      if (this.filled >= CHUNK_SAMPLES) {
        this.flush(false);
      }
    }
    return true;
  }

  flush(last) {
    if (this.filled === 0 && !last) return;
    const pcm = new Int16Array(this.filled);
    for (let i = 0; i < this.filled; i++) {
      // Float32 [-1,1] → Int16 [-32768,32767] with clamp.
      const s = Math.max(-1, Math.min(1, this.buffer[i]));
      pcm[i] = s < 0 ? Math.round(s * 0x8000) : Math.round(s * 0x7fff);
    }
    // Transfer the underlying ArrayBuffer so we don't copy.
    this.port.postMessage(
      { type: "pcm", last, sampleRate: TARGET_SAMPLE_RATE, pcm: pcm.buffer },
      [pcm.buffer],
    );
    this.filled = 0;
  }
}

registerProcessor("pcm-chunker", PcmChunkerProcessor);

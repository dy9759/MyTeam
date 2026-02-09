/**
 * Audio transcription — local whisper first, OpenAI API fallback.
 *
 * Priority:
 * 1. Local whisper/whisper-cli binary (free, no latency, offline)
 * 2. OpenAI Whisper API (requires API key)
 * 3. null (no provider available — placeholder stays for Agent)
 *
 * Called by ChannelManager before the message reaches the Agent,
 * so the Agent only ever sees text.
 *
 * @see docs/channels/media-handling.md — Media processing pipeline and provider priority
 */

import { readFile, unlink } from "node:fs/promises";
import { basename, join } from "node:path";
import { execFile, execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { credentialManager } from "../agent/credentials.js";

/** Cached path to local whisper binary (only caches success; misses re-check each time) */
let cachedWhisperBin: string | undefined;

/** Find local whisper binary in PATH */
function findWhisperBin(): string | false {
  if (cachedWhisperBin !== undefined) return cachedWhisperBin;

  for (const bin of ["whisper", "whisper-cli"]) {
    try {
      execFileSync("which", [bin], { stdio: "pipe" });
      cachedWhisperBin = bin;
      return bin;
    } catch {
      // not found, try next
    }
  }

  // Don't cache failure — whisper may be installed while the process is running
  return false;
}

/**
 * Transcribe audio using local whisper CLI.
 *
 * Runs: whisper "<file>" --model base --output_format txt --output_dir <tmpdir>
 * Reads the generated .txt file and returns its content.
 */
async function transcribeLocal(whisperBin: string, filePath: string): Promise<string> {
  const outDir = tmpdir();

  await new Promise<void>((resolve, reject) => {
    execFile(
      whisperBin,
      [filePath, "--model", "base", "--output_format", "txt", "--output_dir", outDir],
      { timeout: 120000 },
      (err) => (err ? reject(err) : resolve()),
    );
  });

  // whisper outputs <basename_without_ext>.txt
  const name = basename(filePath).replace(/\.[^.]+$/, "");
  const txtPath = join(outDir, `${name}.txt`);
  const text = (await readFile(txtPath, "utf-8")).trim();

  // Clean up the txt file
  await unlink(txtPath).catch(() => {});

  return text;
}

/**
 * Transcribe audio using OpenAI Whisper API.
 */
async function transcribeApi(apiKey: string, filePath: string): Promise<string> {
  const fileBuffer = await readFile(filePath);
  const fileName = basename(filePath);

  // Build multipart form data manually (no external dependency)
  const boundary = `----FormBoundary${Date.now()}`;
  const parts: Buffer[] = [];

  // file field
  parts.push(Buffer.from(
    `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${fileName}"\r\nContent-Type: application/octet-stream\r\n\r\n`,
  ));
  parts.push(fileBuffer);
  parts.push(Buffer.from("\r\n"));

  // model field
  parts.push(Buffer.from(
    `--${boundary}\r\nContent-Disposition: form-data; name="model"\r\n\r\nwhisper-1\r\n`,
  ));

  // closing boundary
  parts.push(Buffer.from(`--${boundary}--\r\n`));

  const body = Buffer.concat(parts);

  const res = await fetch("https://api.openai.com/v1/audio/transcriptions", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": `multipart/form-data; boundary=${boundary}`,
    },
    body,
  });

  if (!res.ok) {
    const errText = await res.text().catch(() => "");
    throw new Error(`Whisper API error: HTTP ${res.status} ${errText}`);
  }

  const result = (await res.json()) as { text: string };
  return result.text;
}

/**
 * Transcribe an audio file.
 *
 * Priority: local whisper → OpenAI API → null.
 *
 * @param filePath - Local path to the audio file
 * @returns Transcribed text, or null if no provider available
 */
export async function transcribeAudio(filePath: string): Promise<string | null> {
  // 1. Try local whisper
  const whisperBin = findWhisperBin();
  if (whisperBin) {
    try {
      return await transcribeLocal(whisperBin, filePath);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[Transcribe] Local whisper failed: ${msg}, trying API...`);
    }
  }

  // 2. Try OpenAI API
  const config = credentialManager.getLlmProviderConfig("openai");
  const apiKey = config?.apiKey;
  if (apiKey) {
    try {
      return await transcribeApi(apiKey, filePath);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[Transcribe] Whisper API failed: ${msg}`);
    }
  }

  // 3. No provider available
  return null;
}

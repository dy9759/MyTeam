// security.ts — validators for the IPC boundary. Pulled out of main.ts
// so vitest can exercise them without spinning up electron. Issues
// #43, #44, #45.

import path from "node:path";
import { realpathSync } from "node:fs";

// isAllowedExternalURL whitelists shell.openExternal targets. Blocks
// `javascript:`, `file:`, `data:` — anything that could exfil tokens or
// drive the OS into a launcher-handler. Issue #44.
export function isAllowedExternalURL(raw: string): boolean {
  if (typeof raw !== "string" || raw === "") return false;
  try {
    const u = new URL(raw);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}

// validateOpenablePath rejects URL-style strings, shell shortcuts, and
// path traversal so a poisoned file_index row or attacker-controlled
// renderer can't walk to /etc/passwd. Returns the realpath when the
// file exists, the resolved input otherwise (lets the caller surface
// the missing-file error). Issue #44.
export function validateOpenablePath(raw: string): string {
  if (typeof raw !== "string" || raw === "") {
    throw new Error("path required");
  }
  if (raw.startsWith("~") || /^[a-z][a-z0-9+.-]*:/i.test(raw)) {
    throw new Error(`path must not be a URL or shortcut: ${raw}`);
  }
  if (raw.split(/[\\/]/).includes("..")) {
    throw new Error(`path must not contain '..': ${raw}`);
  }
  const resolved = path.resolve(raw);
  try {
    return realpathSync(resolved);
  } catch {
    return resolved;
  }
}

// safeIPC wraps a handler so unexpected errors don't crash the renderer.
// Errors are logged with the channel name + re-thrown as a plain Error
// (electron auto-rejects the renderer Promise — caller's catch sees the
// message). Issue #45.
export function safeIPC<TArgs extends unknown[], TResult>(
  channel: string,
  fn: (...args: TArgs) => Promise<TResult>,
): (...args: TArgs) => Promise<TResult> {
  return async (...args: TArgs) => {
    try {
      return await fn(...args);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[ipc:${channel}] ${msg}`);
      throw new Error(msg);
    }
  };
}

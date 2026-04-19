import { describe, expect, it, vi } from "vitest";
import { isAllowedExternalURL, safeIPC, validateOpenablePath } from "./security";

describe("isAllowedExternalURL", () => {
  it.each([
    ["https://example.com", true],
    ["http://localhost:3000", true],
    ["javascript:alert(1)", false],
    ["file:///etc/passwd", false],
    ["data:text/html,<script>", false],
    ["", false],
    ["not a url", false],
  ])("returns %s for %s", (raw, expected) => {
    expect(isAllowedExternalURL(raw)).toBe(expected as boolean);
  });
});

describe("validateOpenablePath", () => {
  it("rejects empty input", () => {
    expect(() => validateOpenablePath("")).toThrow(/path required/);
  });

  it("rejects URL-style schemes", () => {
    expect(() => validateOpenablePath("file:///etc/passwd")).toThrow(/URL or shortcut/);
    expect(() => validateOpenablePath("http://evil/exfil")).toThrow(/URL or shortcut/);
    expect(() => validateOpenablePath("javascript:alert(1)")).toThrow(/URL or shortcut/);
  });

  it("rejects shell shortcut prefix", () => {
    expect(() => validateOpenablePath("~/.ssh/id_rsa")).toThrow(/URL or shortcut/);
  });

  it("rejects path-traversal segments", () => {
    expect(() => validateOpenablePath("/tmp/../etc/passwd")).toThrow(/'\.\.'/);
    expect(() => validateOpenablePath("safe/../escape")).toThrow(/'\.\.'/);
  });

  it("returns the resolved path for a normal absolute path", () => {
    // /tmp exists on macOS + linux; realpathSync should succeed and
    // return either /tmp or /private/tmp depending on the OS.
    const out = validateOpenablePath("/tmp");
    expect(out).toMatch(/tmp$/);
  });

  it("returns the resolved path when realpath fails", () => {
    const out = validateOpenablePath("/tmp/definitely-does-not-exist-12345");
    expect(out).toContain("definitely-does-not-exist-12345");
  });
});

describe("safeIPC", () => {
  it("passes through a successful result", async () => {
    const wrapped = safeIPC("test", async (n: number) => n * 2);
    await expect(wrapped(21)).resolves.toBe(42);
  });

  it("re-throws errors with the original message", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const wrapped = safeIPC("test", async () => {
      throw new Error("kaboom");
    });
    await expect(wrapped()).rejects.toThrow("kaboom");
    expect(errSpy).toHaveBeenCalledWith(expect.stringContaining("[ipc:test]"));
    errSpy.mockRestore();
  });

  it("normalizes non-Error throws to a string message", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const wrapped = safeIPC("test", async () => {
      // eslint-disable-next-line @typescript-eslint/no-throw-literal
      throw "string-thrown";
    });
    await expect(wrapped()).rejects.toThrow("string-thrown");
    errSpy.mockRestore();
  });
});

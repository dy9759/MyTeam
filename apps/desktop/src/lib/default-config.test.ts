import { describe, expect, it } from "vitest";
import { resolveDesktopConfig } from "./default-config";

describe("resolveDesktopConfig", () => {
  it("uses local service defaults in development", () => {
    expect(resolveDesktopConfig("development", {})).toEqual({
      apiBaseUrl: "http://localhost:8080",
      appUrl: "http://localhost:3000",
      wsUrl: "ws://localhost:8080/ws",
    });
  });

  it("prefers explicit environment overrides", () => {
    expect(
      resolveDesktopConfig("development", {
        MYTEAM_API_URL: "https://api.internal.myteam.ai",
        MYTEAM_APP_URL: "https://app.internal.myteam.ai",
        MYTEAM_WS_URL: "wss://ws.internal.myteam.ai/socket",
      }),
    ).toEqual({
      apiBaseUrl: "https://api.internal.myteam.ai",
      appUrl: "https://app.internal.myteam.ai",
      wsUrl: "wss://ws.internal.myteam.ai/socket",
    });
  });

  it("uses hosted defaults in production", () => {
    expect(resolveDesktopConfig("production", {})).toEqual({
      apiBaseUrl: "https://api.myteam.ai",
      appUrl: "https://myteam.ai",
      wsUrl: "wss://api.myteam.ai/ws",
    });
  });
});

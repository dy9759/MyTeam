import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

class FakeWebSocket {
  static OPEN = 1;
  static CLOSED = 3;
  readyState = 0;
  url: string;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({});
  }
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({});
  }
  simulateMessage(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) });
  }

  static instances: FakeWebSocket[] = [];
  static reset() {
    FakeWebSocket.instances = [];
  }
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.useFakeTimers();
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket =
      FakeWebSocket;
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("connects with token and workspace in query string", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "tok-1",
      getWorkspaceId: () => "ws-1",
    });
    client.connect();
    expect(FakeWebSocket.instances[0].url).toBe(
      "ws://api.test/ws?token=tok-1&workspace_id=ws-1",
    );
  });

  it("updates status through connecting → connected", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    const seen: string[] = [];
    client.subscribeStatus((s) => seen.push(s));
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    expect(seen).toEqual(["disconnected", "connecting", "connected"]);
  });

  it("emits parsed events to onEvent", () => {
    const events: unknown[] = [];
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
      onEvent: (msg) => events.push(msg),
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    FakeWebSocket.instances[0].simulateMessage({
      type: "message:created",
      payload: { id: "m1" },
    });
    expect(events).toEqual([
      { type: "message:created", payload: { id: "m1" } },
    ]);
  });

  it("reconnects with exponential backoff capped at 30s", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    client.connect();
    // delays: 1000, 2000, 4000, 8000, 16000, 30000, 30000 (cap holds)
    const expectedDelays = [1000, 2000, 4000, 8000, 16000, 30000, 30000];
    for (let i = 0; i < expectedDelays.length; i++) {
      const delay = expectedDelays[i]!;
      const socket =
        FakeWebSocket.instances[FakeWebSocket.instances.length - 1]!;
      socket.simulateOpen();
      socket.close();
      vi.advanceTimersByTime(delay - 1);
      expect(FakeWebSocket.instances.length).toBe(i + 1);
      vi.advanceTimersByTime(1);
      expect(FakeWebSocket.instances.length).toBe(i + 2);
    }
  });

  it("re-reads token on reconnect", () => {
    let currentToken = "tok-A";
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => currentToken,
      getWorkspaceId: () => "w",
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    currentToken = "tok-B";
    FakeWebSocket.instances[0].close();
    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances[1].url).toContain("token=tok-B");
  });

  it("disconnect cancels pending reconnect", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    FakeWebSocket.instances[0].close();
    client.disconnect();
    vi.advanceTimersByTime(60000);
    expect(FakeWebSocket.instances.length).toBe(1);
  });
});

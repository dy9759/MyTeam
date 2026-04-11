import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useTypingIndicator } from "./use-typing-indicator";

// --- Mocks ---

// Track the registered WS event handler so we can fire it in tests.
let wsHandler: ((payload: unknown) => void) | null = null;
let wsEventType: string | null = null;

vi.mock("@/features/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsEventType = event;
    wsHandler = handler;
  },
}));

vi.mock("@/features/auth", () => ({
  useAuthStore: {
    getState: () => ({
      user: { id: "self-user-id" },
    }),
  },
}));

const sendTypingMock = vi.fn().mockResolvedValue(undefined);
vi.mock("@/shared/api", () => ({
  api: {
    sendTyping: (...args: unknown[]) => sendTypingMock(...args),
  },
}));

describe("useTypingIndicator", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    wsHandler = null;
    wsEventType = null;
    sendTypingMock.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("subscribes to typing WS events", () => {
    renderHook(() => useTypingIndicator({ channelId: "ch-1" }));
    expect(wsEventType).toBe("typing");
    expect(wsHandler).toBeInstanceOf(Function);
  });

  it("tracks typing users from WS events", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    // Simulate a typing event from another user
    act(() => {
      wsHandler!({
        channel_id: "ch-1",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);
  });

  it("filters out self-events", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({
        channel_id: "ch-1",
        sender_id: "self-user-id",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual([]);
  });

  it("filters events by channel_id", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({
        channel_id: "ch-other",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual([]);
  });

  it("filters events by session_id", () => {
    const { result } = renderHook(() => useTypingIndicator({ sessionId: "sess-1" }));

    act(() => {
      wsHandler!({
        session_id: "sess-other",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual([]);

    act(() => {
      wsHandler!({
        session_id: "sess-1",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);
  });

  it("removes typing user when is_typing is false", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({
        channel_id: "ch-1",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);

    act(() => {
      wsHandler!({
        channel_id: "ch-1",
        sender_id: "user-2",
        is_typing: false,
      });
    });

    expect(result.current.typingUsers).toEqual([]);
  });

  it("auto-clears typing state after 3 seconds", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({
        channel_id: "ch-1",
        sender_id: "user-2",
        is_typing: true,
      });
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);

    act(() => {
      vi.advanceTimersByTime(3000);
    });

    expect(result.current.typingUsers).toEqual([]);
  });

  it("tracks multiple typing users", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({ channel_id: "ch-1", sender_id: "user-2", is_typing: true });
      wsHandler!({ channel_id: "ch-1", sender_id: "user-3", is_typing: true });
    });

    expect(result.current.typingUsers).toEqual(["user-2", "user-3"]);
  });

  it("debounces sendTyping calls (max once per second)", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      result.current.sendTyping(true);
    });

    expect(sendTypingMock).toHaveBeenCalledTimes(1);
    expect(sendTypingMock).toHaveBeenCalledWith({
      channel_id: "ch-1",
      session_id: undefined,
      is_typing: true,
    });

    // Calling again immediately should be debounced
    act(() => {
      result.current.sendTyping(true);
    });

    expect(sendTypingMock).toHaveBeenCalledTimes(1);

    // After debounce period, should allow another call
    act(() => {
      vi.advanceTimersByTime(1000);
    });

    act(() => {
      result.current.sendTyping(true);
    });

    expect(sendTypingMock).toHaveBeenCalledTimes(2);
  });

  it("sends stop typing without debounce", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      result.current.sendTyping(true);
    });

    expect(sendTypingMock).toHaveBeenCalledTimes(1);

    // Stop typing should go through even within the debounce window
    act(() => {
      result.current.sendTyping(false);
    });

    expect(sendTypingMock).toHaveBeenCalledTimes(2);
    expect(sendTypingMock).toHaveBeenLastCalledWith({
      channel_id: "ch-1",
      session_id: undefined,
      is_typing: false,
    });
  });

  it("does not add duplicate user to typingUsers", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({ channel_id: "ch-1", sender_id: "user-2", is_typing: true });
    });

    act(() => {
      wsHandler!({ channel_id: "ch-1", sender_id: "user-2", is_typing: true });
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);
  });

  it("resets auto-clear timer when typing event is received again", () => {
    const { result } = renderHook(() => useTypingIndicator({ channelId: "ch-1" }));

    act(() => {
      wsHandler!({ channel_id: "ch-1", sender_id: "user-2", is_typing: true });
    });

    // Advance 2 seconds (less than 3s timeout)
    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(result.current.typingUsers).toEqual(["user-2"]);

    // Send another typing event, resetting the timer
    act(() => {
      wsHandler!({ channel_id: "ch-1", sender_id: "user-2", is_typing: true });
    });

    // Advance 2 more seconds (4 total from first, but only 2 from reset)
    act(() => {
      vi.advanceTimersByTime(2000);
    });

    // Still typing because timer was reset
    expect(result.current.typingUsers).toEqual(["user-2"]);

    // Advance 1 more second (3 total from reset)
    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(result.current.typingUsers).toEqual([]);
  });
});

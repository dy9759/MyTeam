import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { useRetryingPoll } from "./polling";

function PollProbe({ poll }: { poll: () => Promise<void> }) {
  const { error } = useRetryingPoll({
    enabled: true,
    poll,
    fallbackError: "连接丢失",
  });

  return <div>{error ?? "connected"}</div>;
}

describe("useRetryingPoll", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("surfaces failures and retries with backoff instead of hammering", async () => {
    vi.useFakeTimers();

    const poll = vi
      .fn()
      .mockRejectedValueOnce(new Error("socket closed"))
      .mockResolvedValueOnce(undefined)
      .mockResolvedValueOnce(undefined);

    render(<PollProbe poll={poll} />);

    await act(async () => {
      await Promise.resolve();
    });

    expect(poll).toHaveBeenCalledTimes(1);
    expect(screen.getByText("socket closed")).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(2999);
      await Promise.resolve();
    });
    expect(poll).toHaveBeenCalledTimes(1);

    await act(async () => {
      vi.advanceTimersByTime(1);
      await Promise.resolve();
    });
    expect(poll).toHaveBeenCalledTimes(2);

    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByText("connected")).toBeInTheDocument();

    await act(async () => {
      vi.advanceTimersByTime(2999);
      await Promise.resolve();
    });
    expect(poll).toHaveBeenCalledTimes(2);

    await act(async () => {
      vi.advanceTimersByTime(1);
      await Promise.resolve();
    });
    expect(poll).toHaveBeenCalledTimes(3);
  });
});

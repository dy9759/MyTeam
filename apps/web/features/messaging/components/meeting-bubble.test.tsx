import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MeetingBubble, extractSummaryText } from "./meeting-bubble";
import type { ChannelMeeting } from "@/shared/types";

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      members: [{ user_id: "host-1", name: "Alice" }],
    }),
}));

function buildMeeting(overrides: Partial<ChannelMeeting> = {}): ChannelMeeting {
  return {
    id: overrides.id ?? "meeting-1",
    channel_id: "ch-1",
    workspace_id: "ws-1",
    started_by: overrides.started_by ?? "host-1",
    topic: overrides.topic ?? "Weekly sync",
    status: overrides.status ?? "completed",
    notes: "",
    highlights: [],
    started_at: overrides.started_at ?? "2026-04-22T10:00:00.000Z",
    ended_at: overrides.ended_at,
    updated_at: "2026-04-22T10:30:00.000Z",
    audio_duration: overrides.audio_duration,
    failure_reason: overrides.failure_reason,
    summary: overrides.summary,
    ...overrides,
  };
}

describe("extractSummaryText", () => {
  it("returns empty string when summary is missing", () => {
    expect(extractSummaryText(undefined)).toBe("");
    expect(extractSummaryText({})).toBe("");
  });

  it("prefers summary over other keys", () => {
    expect(
      extractSummaryText({ summary: "s", text: "t", content: "c", tldr: "l", abstract: "a" }),
    ).toBe("s");
  });

  it("falls back through text → content → tldr → abstract", () => {
    expect(extractSummaryText({ text: "t", content: "c" })).toBe("t");
    expect(extractSummaryText({ content: "c", tldr: "l" })).toBe("c");
    expect(extractSummaryText({ tldr: "l", abstract: "a" })).toBe("l");
    expect(extractSummaryText({ abstract: "a" })).toBe("a");
  });

  it("skips non-string values", () => {
    expect(extractSummaryText({ summary: 42, text: "t" })).toBe("t");
    expect(extractSummaryText({ summary: null, content: "c" })).toBe("c");
    expect(extractSummaryText({ summary: { nested: "x" }, tldr: "l" })).toBe("l");
  });

  it("skips whitespace-only strings", () => {
    expect(extractSummaryText({ summary: "   ", text: "t" })).toBe("t");
    expect(extractSummaryText({ summary: "", tldr: "l" })).toBe("l");
  });

  it("trims the winning string", () => {
    expect(extractSummaryText({ summary: "  hello  " })).toBe("hello");
  });
});

describe("MeetingBubble", () => {
  it("is clickable only when status === completed", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();

    const { rerender } = render(
      <MeetingBubble meeting={buildMeeting({ status: "recording" })} onOpen={onOpen} />,
    );
    // Status recording → no clickable role.
    expect(screen.queryByRole("button")).toBeNull();

    rerender(<MeetingBubble meeting={buildMeeting({ status: "completed" })} onOpen={onOpen} />);
    const btn = screen.getByRole("button");
    await user.click(btn);
    expect(onOpen).toHaveBeenCalledWith("meeting-1");
  });

  it("fires onOpen on Enter and Space keydown when completed", () => {
    const onOpen = vi.fn();
    render(<MeetingBubble meeting={buildMeeting({ status: "completed" })} onOpen={onOpen} />);
    const btn = screen.getByRole("button");
    fireEvent.keyDown(btn, { key: "Enter" });
    expect(onOpen).toHaveBeenCalledWith("meeting-1");
    fireEvent.keyDown(btn, { key: " " });
    expect(onOpen).toHaveBeenCalledTimes(2);
  });

  it("renders failure_reason when status === failed", () => {
    render(
      <MeetingBubble
        meeting={buildMeeting({ status: "failed", failure_reason: "doubao timeout" })}
        onOpen={() => {}}
      />,
    );
    expect(screen.getByText("doubao timeout")).toBeInTheDocument();
  });

  it("renders summary preview when status === completed and summary has text", () => {
    render(
      <MeetingBubble
        meeting={buildMeeting({ status: "completed", summary: { summary: "We shipped v1.0" } })}
        onOpen={() => {}}
      />,
    );
    expect(screen.getByText("We shipped v1.0")).toBeInTheDocument();
  });

  it("renders status-specific badge label", () => {
    const cases: Array<{ status: ChannelMeeting["status"]; label: string }> = [
      { status: "recording", label: "录制中" },
      { status: "processing", label: "转写中" },
      { status: "completed", label: "已完成" },
      { status: "failed", label: "失败" },
    ];
    for (const { status, label } of cases) {
      const { unmount } = render(
        <MeetingBubble meeting={buildMeeting({ status })} onOpen={() => {}} />,
      );
      expect(screen.getByText(label)).toBeInTheDocument();
      unmount();
    }
  });

  it("resolves host name from workspace members", () => {
    render(
      <MeetingBubble meeting={buildMeeting({ started_by: "host-1" })} onOpen={() => {}} />,
    );
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });
});

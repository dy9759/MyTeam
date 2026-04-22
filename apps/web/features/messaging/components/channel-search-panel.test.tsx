import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChannelSearchPanel } from "./channel-search-panel";
import type { Message } from "@/shared/types/messaging";

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      members: [
        { user_id: "sender-1", name: "Alice" },
        { user_id: "sender-2", name: "Bob" },
      ],
    }),
}));

function buildMessage(overrides: Partial<Message> = {}): Message {
  return {
    id: "message-1",
    workspace_id: "ws-1",
    sender_id: "sender-1",
    sender_type: "member",
    content: "hello world",
    content_type: "text",
    status: "sent",
    created_at: "2026-04-10T00:00:00.000Z",
    updated_at: "2026-04-10T00:00:00.000Z",
    ...overrides,
  } as Message;
}

describe("ChannelSearchPanel", () => {
  it("shows empty-state prompt when query is empty", () => {
    render(
      <ChannelSearchPanel
        messages={[buildMessage({ content: "anything" })]}
        onClose={() => {}}
      />
    );

    expect(
      screen.getByText("输入关键字搜索当前频道的消息。")
    ).toBeInTheDocument();
  });

  it("shows no-match state when query has no results", async () => {
    const user = userEvent.setup();
    render(
      <ChannelSearchPanel
        messages={[buildMessage({ content: "hello world" })]}
        onClose={() => {}}
      />
    );

    await user.type(screen.getByPlaceholderText("输入关键字..."), "zzz");

    expect(screen.getByText("没有匹配的消息。")).toBeInTheDocument();
    expect(screen.getByText("0 条匹配")).toBeInTheDocument();
  });

  it("renders match count and highlighted snippet for query with matches", async () => {
    const user = userEvent.setup();
    const messages = [
      buildMessage({ id: "m1", content: "hello world" }),
      buildMessage({ id: "m2", content: "goodbye" }),
      buildMessage({ id: "m3", content: "hello there" }),
    ];

    render(<ChannelSearchPanel messages={messages} onClose={() => {}} />);

    await user.type(screen.getByPlaceholderText("输入关键字..."), "hello");

    expect(screen.getByText("2 条匹配")).toBeInTheDocument();
    // Two matches render as two <mark> elements with the hit
    const marks = screen.getAllByText("hello");
    expect(marks.length).toBeGreaterThanOrEqual(2);
    expect(marks[0].tagName).toBe("MARK");
  });

  it("renders label with 200-cap suffix when match count exceeds 200", async () => {
    const user = userEvent.setup();
    const messages = Array.from({ length: 250 }, (_, i) =>
      buildMessage({ id: `m${i}`, content: `needle ${i}` })
    );

    render(<ChannelSearchPanel messages={messages} onClose={() => {}} />);

    await user.type(screen.getByPlaceholderText("输入关键字..."), "needle");

    expect(
      screen.getByText("250 条匹配（只显示前 200 条）")
    ).toBeInTheDocument();
  });

  it("renders plain label when match count is at or below 200", async () => {
    const user = userEvent.setup();
    const messages = Array.from({ length: 200 }, (_, i) =>
      buildMessage({ id: `m${i}`, content: `needle ${i}` })
    );

    render(<ChannelSearchPanel messages={messages} onClose={() => {}} />);

    await user.type(screen.getByPlaceholderText("输入关键字..."), "needle");

    expect(screen.getByText("200 条匹配")).toBeInTheDocument();
  });

  it("fires onJumpToMessage with message id when a result is clicked", async () => {
    const user = userEvent.setup();
    const onJumpToMessage = vi.fn();

    render(
      <ChannelSearchPanel
        messages={[
          buildMessage({ id: "target-msg", content: "ping target" }),
        ]}
        onClose={() => {}}
        onJumpToMessage={onJumpToMessage}
      />
    );

    await user.type(screen.getByPlaceholderText("输入关键字..."), "ping");
    await user.click(screen.getByText("ping").closest("button")!);

    expect(onJumpToMessage).toHaveBeenCalledWith("target-msg");
  });

  describe("HighlightedSnippet edge cases", () => {
    it("no leading ellipsis when match is at index 0", async () => {
      const user = userEvent.setup();
      render(
        <ChannelSearchPanel
          messages={[buildMessage({ content: "hello there, longer tail of text follows" })]}
          onClose={() => {}}
        />
      );

      await user.type(screen.getByPlaceholderText("输入关键字..."), "hello");

      const mark = screen.getByText("hello");
      const snippetContainer = mark.parentElement!;
      const firstChildText = snippetContainer.childNodes[0].textContent ?? "";
      expect(firstChildText.startsWith("…")).toBe(false);
    });

    it("no trailing ellipsis when match is at end of text", async () => {
      const user = userEvent.setup();
      const text = "prefix body content needle";
      render(
        <ChannelSearchPanel
          messages={[buildMessage({ content: text })]}
          onClose={() => {}}
        />
      );

      await user.type(screen.getByPlaceholderText("输入关键字..."), "needle");

      const mark = screen.getByText("needle");
      const snippetContainer = mark.parentElement!;
      const lastChildText =
        snippetContainer.childNodes[snippetContainer.childNodes.length - 1].textContent ?? "";
      expect(lastChildText.endsWith("…")).toBe(false);
    });

    it("adds leading + trailing ellipsis when match is beyond radius from both ends", async () => {
      const user = userEvent.setup();
      // 60 chars of padding either side of "needle" -> radius is 40, so
      // both leading and trailing ellipsis should appear.
      const pad = "x".repeat(60);
      const text = `${pad} needle ${pad}`;
      render(
        <ChannelSearchPanel
          messages={[buildMessage({ content: text })]}
          onClose={() => {}}
        />
      );

      await user.type(screen.getByPlaceholderText("输入关键字..."), "needle");

      const mark = screen.getByText("needle");
      const snippetContainer = mark.parentElement!;
      const nodes = snippetContainer.childNodes;
      const firstText = nodes[0].textContent ?? "";
      const lastText = nodes[nodes.length - 1].textContent ?? "";
      expect(firstText.startsWith("…")).toBe(true);
      expect(lastText.endsWith("…")).toBe(true);
    });
  });
});

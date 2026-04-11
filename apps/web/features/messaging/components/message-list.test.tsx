import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageList } from "./message-list";
import type { Message } from "@/shared/types";

function buildMessage(overrides: Partial<Message>): Message {
  return {
    id: overrides.id ?? "message-1",
    workspace_id: overrides.workspace_id ?? "workspace-1",
    sender_id: overrides.sender_id ?? "sender-1",
    sender_type: overrides.sender_type ?? "member",
    content: overrides.content ?? "message content",
    content_type: overrides.content_type ?? "text",
    status: overrides.status ?? "sent",
    created_at: overrides.created_at ?? "2026-04-10T00:00:00.000Z",
    updated_at: overrides.updated_at ?? "2026-04-10T00:00:00.000Z",
    ...overrides,
  };
}

describe("MessageList", () => {
  it("renders thread replies indented under the root message and allows collapsing them", async () => {
    const user = userEvent.setup();
    const root = buildMessage({
      id: "root-message",
      sender_id: "root-sender",
      content: "主话题消息",
      reply_count: 1,
    });
    const reply = buildMessage({
      id: "reply-message",
      sender_id: "reply-sender",
      content: "依附主话题的回复",
      parent_id: root.id,
      thread_id: root.id,
      created_at: "2026-04-10T00:01:00.000Z",
      updated_at: "2026-04-10T00:01:00.000Z",
    });

    render(<MessageList messages={[root, reply]} onOpenThread={() => undefined} />);

    expect(screen.getByText("主话题消息")).toBeInTheDocument();
    expect(screen.getByText("依附主话题的回复")).toBeInTheDocument();
    expect(screen.getByText("依附于主话题展开")).toBeInTheDocument();
    expect(screen.getByText("以下回复依附于这条主话题消息，可随时收起。")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "收起依附于主话题的 1 条回复" }));
    expect(screen.queryByText("依附主话题的回复")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "展开依附于主话题的 1 条回复" }));
    expect(screen.getByText("依附主话题的回复")).toBeInTheDocument();
  });
});

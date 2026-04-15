import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Message } from "@myteam/client-core";
import { MessageList } from "./message-list";

function m(over: Partial<Message>): Message {
  return {
    id: "x",
    workspace_id: "w",
    sender_id: "s",
    sender_type: "member",
    content: "hi",
    content_type: "text",
    status: "sent",
    created_at: "2026-04-15T10:00:00Z",
    updated_at: "2026-04-15T10:00:00Z",
    ...over,
  } as Message;
}

describe("MessageList", () => {
  it("renders owner and agent icons differently", () => {
    render(
      <MessageList
        messages={[
          m({ id: "1", sender_type: "member", content: "hello" }),
          m({ id: "2", sender_type: "agent", content: "world" }),
        ]}
        resolveName={(id, type) => (type === "agent" ? "Bot" : "Alice")}
      />,
    );
    expect(screen.getByText("hello")).toBeInTheDocument();
    expect(screen.getByText("world")).toBeInTheDocument();
    expect(screen.getAllByText("👤").length).toBe(1);
    expect(screen.getAllByText("🤖").length).toBe(1);
  });

  it("renders empty state when no messages", () => {
    render(<MessageList messages={[]} resolveName={() => "X"} />);
    expect(screen.getByText(/no messages/i)).toBeInTheDocument();
  });
});

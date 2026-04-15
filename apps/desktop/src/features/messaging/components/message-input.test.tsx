import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageInput } from "./message-input";

const candidates = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
];

describe("MessageInput", () => {
  it("Enter calls onSend and clears", async () => {
    const onSend = vi.fn().mockResolvedValue(undefined);
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={onSend}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi");
    await userEvent.type(input, "hello{Enter}");
    expect(onSend).toHaveBeenCalledWith("hello");
    expect((input as HTMLTextAreaElement).value).toBe("");
  });

  it("Shift+Enter inserts newline instead of sending", async () => {
    const onSend = vi.fn();
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={onSend}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi") as HTMLTextAreaElement;
    await userEvent.type(input, "line1{Shift>}{Enter}{/Shift}line2");
    expect(onSend).not.toHaveBeenCalled();
    expect(input.value).toBe("line1\nline2");
  });

  it("typing @ shows mention picker; selecting inserts @Name space", async () => {
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={vi.fn()}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi") as HTMLTextAreaElement;
    await userEvent.type(input, "@");
    expect(screen.getByText("Assistant")).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(input.value).toBe("@Assistant ");
  });

  it("disabled while sending is true", () => {
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={vi.fn()}
        sending
      />,
    );
    expect(screen.getByPlaceholderText("Say hi")).toBeDisabled();
  });
});

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MentionPicker } from "./mention-picker";

const candidates = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
  { id: "a2", name: "Bob", kind: "agent" as const },
];

describe("MentionPicker", () => {
  it("filters candidates by query (prefix, case-insensitive)", () => {
    render(
      <MentionPicker
        candidates={candidates}
        query="al"
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByText("Bob")).not.toBeInTheDocument();
  });

  it("Enter selects active candidate", async () => {
    const onSelect = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    await userEvent.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Assistant" }),
    );
  });

  it("Escape calls onClose", async () => {
    const onClose = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={() => {}}
        onClose={onClose}
      />,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("arrow down moves active, enter picks next item", async () => {
    const onSelect = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Alice" }),
    );
  });
});

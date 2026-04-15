import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NewDMDialog } from "./new-dm-dialog";

const items = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
];

describe("NewDMDialog", () => {
  it("filters by name", async () => {
    render(
      <NewDMDialog candidates={items} onSelect={() => {}} onClose={() => {}} />,
    );
    await userEvent.type(screen.getByPlaceholderText(/search/i), "ass");
    expect(screen.getByText("Assistant")).toBeInTheDocument();
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();
  });

  it("click invokes onSelect with peer id + type", async () => {
    const onSelect = vi.fn();
    render(
      <NewDMDialog candidates={items} onSelect={onSelect} onClose={() => {}} />,
    );
    await userEvent.click(screen.getByText("Assistant"));
    expect(onSelect).toHaveBeenCalledWith("a1", "agent");
  });
});

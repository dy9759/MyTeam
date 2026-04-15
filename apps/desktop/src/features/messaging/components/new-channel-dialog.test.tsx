import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NewChannelDialog } from "./new-channel-dialog";

describe("NewChannelDialog", () => {
  it("submit button disabled when name blank", () => {
    render(<NewChannelDialog onCreate={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: /create/i })).toBeDisabled();
  });

  it("submit calls onCreate with name", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<NewChannelDialog onCreate={onCreate} onClose={vi.fn()} />);
    await userEvent.type(screen.getByPlaceholderText(/channel name/i), "general");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    expect(onCreate).toHaveBeenCalledWith("general");
  });
});

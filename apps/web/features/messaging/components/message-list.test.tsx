import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageList } from "./message-list";
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store";

// Mock workspace store so resolveDisplayName works
vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({ members: [{ user_id: "sender-1", name: "Alice" }] }),
}));

function buildMessage(overrides: Partial<{
  id: string;
  sender_id: string;
  sender_type: string;
  content: string;
  created_at: string;
  reply_count: number;
  is_impersonated: boolean;
}>) {
  return {
    id: overrides.id ?? "message-1",
    sender_id: overrides.sender_id ?? "sender-1",
    sender_type: overrides.sender_type ?? "member",
    content: overrides.content ?? "message content",
    created_at: overrides.created_at ?? "2026-04-10T00:00:00.000Z",
    ...overrides,
  };
}

describe("MessageList", () => {
  it("renders thread chip for roots with replies and the hover icon triggers onOpenThread", async () => {
    const user = userEvent.setup();
    const onOpenThread = vi.fn();

    const root = buildMessage({
      id: "root-message",
      sender_id: "sender-1",
      content: "Hello world",
      reply_count: 3,
    });

    const second = buildMessage({
      id: "second-message",
      sender_id: "sender-2",
      content: "Another message",
    });

    render(
      <MessageList
        messages={[root, second]}
        onOpenThread={onOpenThread}
      />
    );

    // Both messages render their content
    expect(screen.getByText("Hello world")).toBeInTheDocument();
    expect(screen.getByText("Another message")).toBeInTheDocument();

    // Reply chip surfaces the count — it now toggles inline expansion
    // instead of opening the side panel, so we assert the chip exists
    // and the side-panel trigger is wired to the hover icon instead.
    expect(screen.getByText(/3 条回复/)).toBeInTheDocument();

    const sidePanelTriggers = screen.getAllByTitle("在侧栏打开讨论串");
    // Root message renders first, so its hover icon sits at index 0.
    await user.click(sidePanelTriggers[0]);
    expect(onOpenThread).toHaveBeenCalledWith("root-message");
  });

  describe("file attachment", () => {
    beforeEach(() => {
      useFileViewerStore.getState().close();
    });

    it("clicking a file chip opens the file viewer with full target metadata", async () => {
      const user = userEvent.setup();
      const msg = {
        ...buildMessage({ id: "m-file", content: "see attached" }),
        file_id: "att-123",
        file_name: "report.md",
        file_size: 4096,
        file_content_type: "text/markdown",
      };

      render(<MessageList messages={[msg]} />);

      const chip = screen.getByTitle("预览文件");
      expect(chip.tagName).toBe("BUTTON");
      expect(chip).toHaveTextContent("report.md");
      expect(useFileViewerStore.getState().active).toBeNull();

      await user.click(chip);

      expect(useFileViewerStore.getState().active).toEqual({
        file_id: "att-123",
        file_name: "report.md",
        file_size: 4096,
        file_content_type: "text/markdown",
      });
    });

    it("renders non-interactive chip when file_id is missing", () => {
      const msg = {
        ...buildMessage({ id: "m-legacy", content: "legacy attachment" }),
        file_name: "legacy.md",
      };

      render(<MessageList messages={[msg]} />);

      expect(screen.queryByTitle("预览文件")).toBeNull();
      expect(screen.getByText(/legacy\.md/)).toBeInTheDocument();
    });
  });
});

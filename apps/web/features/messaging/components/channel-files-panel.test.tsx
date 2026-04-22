import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChannelFilesPanel } from "./channel-files-panel";
import type { Message } from "@/shared/types/messaging";
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store";

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      members: [
        { user_id: "user-1", name: "Alice" },
        { user_id: "user-2", name: "Bob" },
      ],
    }),
}));

function buildMsg(overrides: Partial<Message> & { id: string; created_at: string }): Message {
  return {
    id: overrides.id,
    workspace_id: "ws-1",
    sender_id: overrides.sender_id ?? "user-1",
    sender_type: "member",
    content: overrides.content ?? "",
    content_type: "file",
    status: "sent",
    created_at: overrides.created_at,
    updated_at: overrides.created_at,
    ...overrides,
  };
}

describe("ChannelFilesPanel", () => {
  beforeEach(() => {
    useFileViewerStore.getState().close();
  });

  it("shows empty state when no messages have file attachments", () => {
    const messages = [buildMsg({ id: "m1", created_at: "2026-04-20T00:00:00.000Z" })];
    render(<ChannelFilesPanel messages={messages} onClose={() => {}} />);
    expect(screen.getByText("当前频道还没有上传的文件。")).toBeInTheDocument();
  });

  it("sorts attachments newest-first by created_at", () => {
    const messages: Message[] = [
      buildMsg({
        id: "m-old",
        created_at: "2026-04-20T00:00:00.000Z",
        file_id: "f-old",
        file_name: "old.md",
      }),
      buildMsg({
        id: "m-new",
        created_at: "2026-04-22T00:00:00.000Z",
        file_id: "f-new",
        file_name: "new.md",
      }),
      buildMsg({
        id: "m-mid",
        created_at: "2026-04-21T00:00:00.000Z",
        file_id: "f-mid",
        file_name: "mid.md",
      }),
    ];
    render(<ChannelFilesPanel messages={messages} onClose={() => {}} />);
    const buttons = screen.getAllByTitle("打开文件预览");
    expect(buttons.map((b) => b.textContent)).toEqual([
      expect.stringContaining("new.md"),
      expect.stringContaining("mid.md"),
      expect.stringContaining("old.md"),
    ]);
  });

  it("click opens file viewer with full target metadata", async () => {
    const user = userEvent.setup();
    const messages: Message[] = [
      buildMsg({
        id: "m1",
        created_at: "2026-04-22T00:00:00.000Z",
        file_id: "att-xyz",
        file_name: "report.pdf",
        file_size: 2048,
        file_content_type: "application/pdf",
      }),
    ];
    render(<ChannelFilesPanel messages={messages} onClose={() => {}} />);
    expect(useFileViewerStore.getState().active).toBeNull();
    await user.click(screen.getByTitle("打开文件预览"));
    expect(useFileViewerStore.getState().active).toEqual({
      file_id: "att-xyz",
      file_name: "report.pdf",
      file_size: 2048,
      file_content_type: "application/pdf",
    });
  });

  it("falls back to first 8 chars of sender_id when member is not in workspace", () => {
    const messages: Message[] = [
      buildMsg({
        id: "m1",
        created_at: "2026-04-22T00:00:00.000Z",
        sender_id: "unknown-user-id-abcdef",
        file_id: "f1",
        file_name: "a.md",
      }),
    ];
    render(<ChannelFilesPanel messages={messages} onClose={() => {}} />);
    expect(screen.getByText(/unknown-/)).toBeInTheDocument();
  });

  it("close button fires onClose", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<ChannelFilesPanel messages={[]} onClose={onClose} />);
    await user.click(screen.getByTitle("关闭"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

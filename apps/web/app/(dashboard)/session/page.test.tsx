import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

const mockLoadConversations = vi.fn();
const mockLoadMessages = vi.fn();
const mockSendMessage = vi.fn();
const mockFetchChannels = vi.fn();
const mockFetchChannel = vi.fn();
const mockFetchMembers = vi.fn();

vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("id=agent-1&type=dm"),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), prefetch: vi.fn() }),
}));

vi.mock("@/features/messaging/store", () => ({
  useMessagingStore: (selector: (s: any) => any) =>
    selector({
      conversations: [
        {
          peer_id: "agent-1",
          peer_type: "agent",
          peer_name: "Personal Agent",
          unread_count: 0,
        },
      ],
      currentMessages: [],
      loading: false,
      loadConversations: mockLoadConversations,
      loadMessages: mockLoadMessages,
      sendMessage: mockSendMessage,
    }),
}));

vi.mock("@/features/channels/store", () => ({
  useChannelStore: (selector: (s: any) => any) =>
    selector({
      channels: [],
      currentChannel: null,
      members: [],
      fetch: mockFetchChannels,
      fetchChannel: mockFetchChannel,
      fetchMembers: mockFetchMembers,
    }),
}));

vi.mock("@/features/inbox", () => ({
  useInboxStore: (selector: (s: any) => any) =>
    selector({
      dedupedItems: () => [],
      loading: false,
      unreadCount: () => 0,
    }),
}));

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      agents: [
        {
          id: "agent-1",
          name: "Personal Agent",
          display_name: "Personal Agent",
          agent_type: "personal_agent",
          owner_id: "user-1",
        },
      ],
    }),
}));

vi.mock("@/features/auth", () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({
      user: { id: "user-1" },
    }),
}));

vi.mock("@/features/messaging/stores/selection-store", () => ({
  useMessageSelectionStore: (selector: (s: any) => any) =>
    selector({
      setScope: vi.fn(),
      clear: vi.fn(),
      selectedIds: new Set(),
    }),
}));

vi.mock("@/features/messaging/components/message-list", () => ({
  MessageList: () => <div>message-list</div>,
}));

vi.mock("@/features/messaging/components/message-input", () => ({
  MessageInput: () => <div>message-input</div>,
}));

vi.mock("@/features/messaging/components/thread-panel", () => ({
  ThreadPanel: () => null,
}));

vi.mock("@/features/messaging/components/generate-project-button", () => ({
  GenerateProjectButton: () => null,
}));

vi.mock("@/features/messaging/components/promote-to-channel-button", () => ({
  PromoteToChannelButton: () => null,
}));

vi.mock("@/features/messaging/components/invite-channel-member-dialog", () => ({
  InviteChannelMemberDialog: () => null,
}));

vi.mock("@/features/messaging/stores/archive-store", () => ({
  useConversationArchiveStore: (selector: (s: any) => any) =>
    selector({
      archivedKeys: new Set<string>(),
      fetch: vi.fn(),
      archive: vi.fn(),
      unarchive: vi.fn(),
    }),
}));

vi.mock("@/features/messaging/hooks/use-typing-indicator", () => ({
  useTypingIndicator: () => ({ typingUsers: [], sendTyping: vi.fn() }),
}));

vi.mock("@/features/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("@/shared/api", () => ({
  api: {
    getChannelMessages: vi.fn(),
    createChannel: vi.fn(),
    sendMessage: vi.fn(),
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import SessionPage from "./page";

describe("SessionPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("loads agent DMs with peer_type=agent", async () => {
    render(<SessionPage />);

    await waitFor(() => {
      expect(mockLoadMessages).toHaveBeenCalledWith({
        recipient_id: "agent-1",
        peer_type: "agent",
      });
    });
  });
});

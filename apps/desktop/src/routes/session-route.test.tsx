import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@myteam/client-core";
import { SessionRoute } from "./session-route";

const mocks = vi.hoisted(() => ({
  getPersonalAgent: vi.fn(),
  loadConversations: vi.fn(),
  loadChannels: vi.fn(),
  loadMessages: vi.fn(),
  sendMessage: vi.fn(),
  createChannel: vi.fn(),
  workspaceState: {
    workspace: { id: "ws-1" },
    agents: [],
    members: [],
  },
  messagingState: {
    currentMessages: [],
    sending: false,
    loadConversations: vi.fn(),
    loadChannels: vi.fn(),
    loadMessages: vi.fn(),
    sendMessage: vi.fn(),
    createChannel: vi.fn(),
    channels: [],
    conversations: [],
  },
}));

vi.mock("@/lib/desktop-client", () => ({
  desktopApi: {
    getPersonalAgent: mocks.getPersonalAgent,
  },
  useDesktopWorkspaceStore: (
    selector: (state: typeof mocks.workspaceState) => unknown,
  ) => selector(mocks.workspaceState),
}));

vi.mock("@/features/messaging", () => ({
  FileList: () => <div>Files</div>,
  MessageInput: () => <div>Message input</div>,
  MessageList: () => <div>Message list</div>,
  NewChannelDialog: () => null,
  NewDMDialog: () => null,
  ThreadPanel: () => <div>Thread</div>,
  TypingIndicator: () => <div>Typing</div>,
  useDesktopMessagingStore: () => mocks.messagingState,
}));

function personalAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    owner_id: "user-1",
    owner_type: "user",
    agent_type: "personal_agent",
    scope: null,
    name: "Assistant",
    description: "",
    instructions: "",
    avatar_url: null,
    visibility: "private",
    status: "offline",
    max_concurrent_tasks: 1,
    skills: [],
    created_at: "",
    updated_at: "",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("SessionRoute", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPersonalAgent.mockResolvedValue(personalAgent());
    mocks.messagingState.currentMessages = [];
    mocks.messagingState.sending = false;
    mocks.messagingState.loadConversations = mocks.loadConversations;
    mocks.messagingState.loadChannels = mocks.loadChannels;
    mocks.messagingState.loadMessages = mocks.loadMessages;
    mocks.messagingState.sendMessage = mocks.sendMessage;
    mocks.messagingState.createChannel = mocks.createChannel;
    mocks.messagingState.channels = [];
    mocks.messagingState.conversations = [];
  });

  it("loads the personal agent and renders it as an available DM", async () => {
    render(<SessionRoute />);

    await waitFor(() => {
      expect(mocks.getPersonalAgent).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByText("Assistant")).toBeInTheDocument();
    expect(screen.getByText("agent · available")).toBeInTheDocument();
  });
});

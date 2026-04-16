import { useEffect, useMemo, useState } from "react";
import type { Channel, Conversation } from "@myteam/client-core";
import { RouteShell } from "@/components/route-shell";
import { useDesktopWorkspaceStore } from "@/lib/desktop-client";
import {
  MessageInput,
  MessageList,
  NewChannelDialog,
  NewDMDialog,
  TypingIndicator,
  useDesktopMessagingStore,
  type DMCandidate,
} from "@/features/messaging";

type Selection =
  | { kind: "channel"; channel: Channel }
  | { kind: "dm"; conversation: Conversation };

export function SessionRoute() {
  const agents = useDesktopWorkspaceStore((s) => s.agents);
  const members = useDesktopWorkspaceStore((s) => s.members);

  const {
    currentMessages,
    sending,
    loadConversations,
    loadChannels,
    loadMessages,
    sendMessage,
    createChannel,
    channels,
    conversations,
  } = useDesktopMessagingStore();

  const [selection, setSelection] = useState<Selection | null>(null);
  const [showNewDM, setShowNewDM] = useState(false);
  const [showNewChannel, setShowNewChannel] = useState(false);
  const [typingAgent, setTypingAgent] = useState<string | null>(null);

  const mentionCandidates = useMemo(() => {
    const personalAgents = agents.filter(
      (a) => !((a as any).agent_type === "system_agent" || (a as any).agent_type === "page_system_agent" || (a as any).is_system)
    );
    return [
      ...personalAgents.map((a) => ({
        id: a.id,
        name: a.name,
        kind: "agent" as const,
      })),
      ...members.map((m) => ({
        id: m.id,
        name: m.name,
        kind: "owner" as const,
      })),
    ];
  }, [agents, members]);

  const dmCandidates: DMCandidate[] = mentionCandidates;

  useEffect(() => {
    void loadConversations();
    void loadChannels();
  }, [loadConversations, loadChannels]);

  useEffect(() => {
    if (!selection) return;
    if (selection.kind === "channel") {
      void loadMessages({ channel_id: selection.channel.id });
    } else {
      void loadMessages({ recipient_id: selection.conversation.peer_id });
    }
  }, [selection, loadMessages]);

  const resolveName = (senderId: string, senderType: "member" | "agent") => {
    if (senderType === "agent") {
      return agents.find((a) => a.id === senderId)?.name ?? "Agent";
    }
    return members.find((m) => m.id === senderId)?.name ?? "User";
  };

  const placeholder =
    selection?.kind === "channel"
      ? `Message # ${selection.channel.name}`
      : selection?.kind === "dm"
      ? `Message ${selection.conversation.peer_name ?? selection.conversation.peer_id}`
      : "Select a conversation";

  const handleSend = async (text: string) => {
    if (!selection) return;
    // Set typing indicator for agent DMs
    if (selection.kind === "dm" && selection.conversation.peer_type === "agent") {
      setTypingAgent(selection.conversation.peer_name ?? "Agent");
    }
    if (selection.kind === "channel") {
      await sendMessage({ channel_id: selection.channel.id, content: text });
    } else {
      await sendMessage({
        recipient_id: selection.conversation.peer_id,
        recipient_type: selection.conversation.peer_type,
        content: text,
      });
    }
  };

  useEffect(() => {
    if (!typingAgent) return;
    const lastMsg = currentMessages[currentMessages.length - 1];
    if (lastMsg && lastMsg.sender_type === "agent") {
      setTypingAgent(null);
    }
  }, [currentMessages, typingAgent]);

  return (
    <RouteShell
      eyebrow="Session"
      title="Collaborate with agents and teammates"
      description="Send messages, mention agents, and watch replies stream in real time."
    >
      <div className="grid min-h-[70vh] gap-4 xl:grid-cols-[260px_1fr]">
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setShowNewDM(true)}
              className="flex-1 rounded-2xl bg-primary px-3 py-2 text-xs font-medium text-primary-foreground"
            >
              + New DM
            </button>
            <button
              type="button"
              onClick={() => setShowNewChannel(true)}
              className="flex-1 rounded-2xl border border-border/70 px-3 py-2 text-xs font-medium text-foreground"
            >
              + Channel
            </button>
          </div>
          <p className="mt-5 px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Channels
          </p>
          <div className="mt-2 space-y-1">
            {channels.map((channel) => (
              <SidebarItem
                key={channel.id}
                active={selection?.kind === "channel" && selection.channel.id === channel.id}
                onClick={() => setSelection({ kind: "channel", channel })}
                title={`# ${channel.name}`}
              />
            ))}
          </div>
          <p className="mt-5 px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Direct Messages
          </p>
          <div className="mt-2 space-y-1">
            {conversations.map((conversation) => (
              <SidebarItem
                key={`${conversation.peer_type}:${conversation.peer_id}`}
                active={
                  selection?.kind === "dm" &&
                  selection.conversation.peer_id === conversation.peer_id
                }
                onClick={() => setSelection({ kind: "dm", conversation })}
                title={conversation.peer_name ?? conversation.peer_id}
                subtitle={conversation.peer_type}
              />
            ))}
          </div>
        </section>

        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="border-b border-border/70 px-2 pb-4">
            <h3 className="text-xl font-medium text-foreground">
              {selection?.kind === "channel"
                ? `# ${selection.channel.name}`
                : selection?.kind === "dm"
                ? selection.conversation.peer_name ?? selection.conversation.peer_id
                : "Select a conversation"}
            </h3>
            {selection?.kind === "dm" && selection.conversation.peer_type === "agent" && (
              <p className="mt-2 rounded-xl bg-amber-500/10 px-3 py-1.5 text-xs text-amber-300">
                跨 owner 对话对双方 owner 可见
              </p>
            )}
          </div>
          <div className="flex-1 overflow-hidden py-4">
            {selection ? (
              <MessageList messages={currentMessages} resolveName={resolveName} />
            ) : (
              <EmptyPane message="Pick a channel or DM on the left, or start a new one." />
            )}
          </div>
          {typingAgent ? <TypingIndicator agentName={typingAgent} /> : null}
          {selection ? (
            <MessageInput
              placeholder={placeholder}
              candidates={mentionCandidates}
              onSend={handleSend}
              sending={sending}
            />
          ) : null}
        </section>
      </div>

      {showNewDM ? (
        <NewDMDialog
          candidates={dmCandidates}
          onSelect={(peerId, peerType) => {
            setShowNewDM(false);
            const peer = mentionCandidates.find((c) => c.id === peerId);
            setSelection({
              kind: "dm",
              conversation: {
                peer_id: peerId,
                peer_type: peerType,
                peer_name: peer?.name,
                unread_count: 0,
              } as Conversation,
            });
          }}
          onClose={() => setShowNewDM(false)}
        />
      ) : null}
      {showNewChannel ? (
        <NewChannelDialog
          onCreate={async (name) => {
            const ch = await createChannel({ name, visibility: "private" });
            setShowNewChannel(false);
            setSelection({ kind: "channel", channel: ch });
          }}
          onClose={() => setShowNewChannel(false)}
        />
      ) : null}
    </RouteShell>
  );
}

function SidebarItem({
  active,
  onClick,
  title,
  subtitle,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  subtitle?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full rounded-2xl px-3 py-2 text-left transition ${
        active ? "bg-primary text-primary-foreground" : "hover:bg-white/5"
      }`}
    >
      <p className="truncate text-sm font-medium">{title}</p>
      {subtitle ? (
        <p
          className={`mt-1 text-xs ${
            active ? "text-primary-foreground/70" : "text-muted-foreground"
          }`}
        >
          {subtitle}
        </p>
      ) : null}
    </button>
  );
}

function EmptyPane({ message }: { message: string }) {
  return (
    <div className="flex h-full items-center justify-center rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
      {message}
    </div>
  );
}

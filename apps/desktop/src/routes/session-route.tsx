import { useEffect, useState } from "react";
import type { Conversation, Message, Channel } from "@myteam/client-core";
import { RouteShell } from "@/components/route-shell";
import { desktopApi, useDesktopWorkspaceStore } from "@/lib/desktop-client";

type Selection =
  | { kind: "dm"; conversation: Conversation }
  | { kind: "channel"; channel: Channel };

export function SessionRoute() {
  const snapshot = useDesktopWorkspaceStore((state) => state.workspaceSnapshot);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);

  useEffect(() => {
    if (!selection) return;
    if (selection.kind === "channel") {
      void desktopApi
        .listMessages({ channel_id: selection.channel.id, limit: 100, offset: 0 })
        .then((response) => setMessages(response.messages as Message[]));
      return;
    }

    void desktopApi
      .listMessages({ recipient_id: selection.conversation.peer_id, limit: 100, offset: 0 })
      .then((response) => setMessages(response.messages as Message[]));
  }, [selection]);

  useEffect(() => {
    if (snapshot?.channels?.[0]) {
      setSelection((current) => current ?? { kind: "channel", channel: snapshot.channels[0] });
      return;
    }
    if (snapshot?.conversations?.[0]) {
      setSelection((current) => current ?? { kind: "dm", conversation: snapshot.conversations[0] });
    }
  }, [snapshot]);

  return (
    <RouteShell
      eyebrow="Session"
      title="Desktop workspace collaboration"
      description="This is the first desktop pass of Session: channels and DMs in one shell, backed by the same backend conversations and messages APIs."
    >
      <div className="grid min-h-[70vh] gap-4 xl:grid-cols-[260px_1fr_320px]">
        <section className="rounded-[28px] border border-border/70 bg-card/85 p-4">
          <p className="px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Channels
          </p>
          <div className="mt-3 space-y-1">
            {(snapshot?.channels ?? []).map((channel) => (
              <SelectionButton
                key={channel.id}
                active={selection?.kind === "channel" && selection.channel.id === channel.id}
                onClick={() => setSelection({ kind: "channel", channel })}
                title={channel.name}
                subtitle={channel.visibility ?? "channel"}
              />
            ))}
          </div>
          <p className="mt-6 px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            DMs
          </p>
          <div className="mt-3 space-y-1">
            {(snapshot?.conversations ?? []).map((conversation) => (
              <SelectionButton
                key={`${conversation.peer_type}:${conversation.peer_id}`}
                active={selection?.kind === "dm" && selection.conversation.peer_id === conversation.peer_id}
                onClick={() => setSelection({ kind: "dm", conversation })}
                title={conversation.peer_name ?? conversation.peer_id}
                subtitle={`${conversation.peer_type} · ${conversation.unread_count ?? 0} unread`}
              />
            ))}
          </div>
        </section>

        <section className="rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="border-b border-border/70 px-2 pb-4">
            <p className="text-xs uppercase tracking-[0.2em] text-muted-foreground">
              Active thread
            </p>
            <h3 className="mt-2 text-xl font-medium text-foreground">
              {selection?.kind === "channel"
                ? `# ${selection.channel.name}`
                : selection?.conversation.peer_name ?? "Select a conversation"}
            </h3>
          </div>
          <div className="mt-4 space-y-3">
            {messages.length === 0 ? (
              <EmptyPane message="No messages loaded for this conversation yet." />
            ) : (
              messages.map((message) => (
                <article
                  key={message.id}
                  className={`rounded-3xl border border-border/70 px-4 py-3 ${
                    message.parent_id ? "ml-6 bg-white/[0.02]" : "bg-background/70"
                  }`}
                >
                  <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
                    {message.sender_type}
                  </p>
                  <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-foreground">
                    {message.content}
                  </p>
                </article>
              ))
            )}
          </div>
        </section>

        <section className="rounded-[28px] border border-border/70 bg-card/85 p-4">
          <p className="px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Workspace substrate
          </p>
          <div className="mt-4 space-y-3">
            <StatCard label="Browser tabs" value={String(snapshot?.browser_tabs.length ?? 0)} />
            <StatCard label="Browser contexts" value={String(snapshot?.browser_contexts.length ?? 0)} />
            <StatCard label="Collaborators" value={String(snapshot?.collaborators.length ?? 0)} />
            <StatCard label="Unread inbox" value={String(snapshot?.inbox.unread_count ?? 0)} />
          </div>
        </section>
      </div>
    </RouteShell>
  );
}

function SelectionButton({
  active,
  onClick,
  title,
  subtitle,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  subtitle: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full rounded-2xl px-3 py-3 text-left transition ${
        active ? "bg-primary text-primary-foreground" : "hover:bg-white/5"
      }`}
    >
      <p className="truncate text-sm font-medium">{title}</p>
      <p className={`mt-1 text-xs ${active ? "text-primary-foreground/70" : "text-muted-foreground"}`}>
        {subtitle}
      </p>
    </button>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-border/70 bg-background/70 px-4 py-3">
      <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">{label}</p>
      <p className="mt-2 text-lg font-semibold text-foreground">{value}</p>
    </div>
  );
}

function EmptyPane({ message }: { message: string }) {
  return (
    <div className="rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
      {message}
    </div>
  );
}

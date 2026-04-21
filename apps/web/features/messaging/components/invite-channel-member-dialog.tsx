"use client";

import { useMemo, useState } from "react";
import { Bot, Check, Plus, User, X } from "lucide-react";
import { toast } from "sonner";

import { useWorkspaceStore } from "@/features/workspace";
import { useChannelStore } from "@/features/channels/store";
import { api } from "@/shared/api";

interface Props {
  channelId: string;
  channelName: string;
  open: boolean;
  onClose: () => void;
}

// InviteChannelMemberDialog lists workspace members and agents. Entries
// already in the channel show a disabled "Joined" chip; the rest have an
// "Invite" action that calls POST /api/channels/{id}/members and refreshes
// the members list in place.
export function InviteChannelMemberDialog({ channelId, channelName, open, onClose }: Props) {
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const channelMembers = useChannelStore((s) => s.members);
  const fetchMembers = useChannelStore((s) => s.fetchMembers);

  const [query, setQuery] = useState("");
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const existing = useMemo(() => {
    const set = new Set<string>();
    for (const m of channelMembers) {
      set.add(`${m.member_type}:${m.member_id}`);
    }
    return set;
  }, [channelMembers]);

  const q = query.trim().toLowerCase();
  const matchedMembers = useMemo(
    () =>
      members.filter((m) => {
        const name = (m.name || m.email || m.user_id || "").toLowerCase();
        return !q || name.includes(q);
      }),
    [members, q],
  );
  const matchedAgents = useMemo(
    () => agents.filter((a) => !q || (a.display_name || a.name).toLowerCase().includes(q)),
    [agents, q],
  );

  if (!open) return null;

  const invite = async (id: string, type: "member" | "agent", displayName: string) => {
    const key = `${type}:${id}`;
    setBusyKey(key);
    try {
      await api.inviteChannelMember(channelId, { member_id: id, member_type: type });
      toast.success(`已邀请 ${displayName} 加入 #${channelName}`);
      await fetchMembers(channelId);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setBusyKey(null);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-background rounded-lg shadow-lg w-full max-w-md p-5">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold text-foreground">邀请加入 #{channelName}</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索成员或 agent…"
          className="mt-4 w-full px-2 py-1.5 rounded-md border border-border bg-background text-[13px] text-foreground"
        />

        <div className="mt-3 max-h-80 overflow-auto space-y-1">
          {matchedMembers.length === 0 && matchedAgents.length === 0 && (
            <p className="text-[12px] text-muted-foreground py-6 text-center">无匹配</p>
          )}

          {matchedMembers.length > 0 && (
            <>
              <p className="text-[11px] uppercase tracking-wider text-muted-foreground/70 pt-1">
                成员
              </p>
              {matchedMembers.map((m) => {
                const id = m.user_id;
                const name = m.name || m.email || id;
                const joined = existing.has(`member:${id}`);
                const busy = busyKey === `member:${id}`;
                return (
                  <div
                    key={`m-${id}`}
                    className="flex items-center justify-between gap-2 px-2 py-1.5 rounded-md hover:bg-accent"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <User className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                      <span className="text-[13px] text-foreground truncate">{name}</span>
                    </div>
                    {joined ? (
                      <span className="text-[11px] text-muted-foreground flex items-center gap-1">
                        <Check className="h-3 w-3" />
                        已加入
                      </span>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => invite(id, "member", name)}
                        className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] font-medium bg-primary text-primary-foreground disabled:opacity-50 hover:opacity-90"
                      >
                        <Plus className="h-3 w-3" />
                        {busy ? "邀请中…" : "邀请"}
                      </button>
                    )}
                  </div>
                );
              })}
            </>
          )}

          {matchedAgents.length > 0 && (
            <>
              <p className="text-[11px] uppercase tracking-wider text-muted-foreground/70 pt-3">
                Agents
              </p>
              {matchedAgents.map((a) => {
                const name = a.display_name || a.name;
                const joined = existing.has(`agent:${a.id}`);
                const busy = busyKey === `agent:${a.id}`;
                return (
                  <div
                    key={`a-${a.id}`}
                    className="flex items-center justify-between gap-2 px-2 py-1.5 rounded-md hover:bg-accent"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <Bot className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                      <span className="text-[13px] text-foreground truncate">{name}</span>
                    </div>
                    {joined ? (
                      <span className="text-[11px] text-muted-foreground flex items-center gap-1">
                        <Check className="h-3 w-3" />
                        已加入
                      </span>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => invite(a.id, "agent", name)}
                        className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] font-medium bg-primary text-primary-foreground disabled:opacity-50 hover:opacity-90"
                      >
                        <Plus className="h-3 w-3" />
                        {busy ? "邀请中…" : "邀请"}
                      </button>
                    )}
                  </div>
                );
              })}
            </>
          )}
        </div>

        <div className="mt-5 flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="px-3 h-8 rounded-md text-[12px] font-medium text-muted-foreground hover:text-foreground"
          >
            完成
          </button>
        </div>
      </div>
    </div>
  );
}

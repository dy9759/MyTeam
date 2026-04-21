"use client";

import { useState } from "react";
import { Hash } from "lucide-react";
import { useRouter } from "next/navigation";

import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { useChannelStore } from "@/features/channels/store";
import { api } from "@/shared/api";

interface Props {
  peerId: string;
  peerType: "member" | "agent";
  peerName: string;
}

// PromoteToChannelButton turns the current DM (optionally narrowed to a
// message subset via the selection store) into a new channel. Copies the
// selected messages into the channel so the history isn't lost, and adds
// both parties as members.
export function PromoteToChannelButton({ peerId, peerType, peerName }: Props) {
  const router = useRouter();
  const selectedIds = useMessageSelectionStore((s) => s.selectedIds);
  const clearSelection = useMessageSelectionStore((s) => s.clear);
  const fetchChannels = useChannelStore((s) => s.fetch);

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const count = selectedIds.size;

  const openDialog = () => {
    setName(`${peerName}-channel`);
    setError(null);
    setOpen(true);
  };

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("name is required");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const channel = await api.createChannelFromDM({
        name: trimmed,
        peer_id: peerId,
        peer_type: peerType,
        message_ids: count > 0 ? Array.from(selectedIds) : undefined,
      });
      clearSelection();
      await fetchChannels();
      setOpen(false);
      router.push(`/session?type=channel&id=${channel.id}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <button
        type="button"
        onClick={openDialog}
        title={count > 0 ? `Promote ${count} message(s) to a new channel` : "Create a channel from this chat"}
        className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
      >
        <Hash className="h-3.5 w-3.5" />
        转为频道
        {count > 0 && (
          <span className="ml-1 text-[10px] px-1 rounded bg-primary/20 text-primary">
            {count}
          </span>
        )}
      </button>

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-background rounded-lg shadow-lg w-full max-w-md p-5">
            <h2 className="text-base font-semibold text-foreground">转为频道</h2>
            <p className="text-[12px] text-muted-foreground mt-1">
              {count > 0
                ? `将选中的 ${count} 条消息复制到新频道,并邀请 ${peerName} 加入。`
                : `创建一个空频道并邀请 ${peerName} 加入。`}
            </p>

            <label className="block text-[12px] font-medium text-foreground mt-4">频道名称</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={submitting}
              autoFocus
              className="mt-1 w-full px-2 py-1.5 rounded-md border border-border bg-background text-[13px] text-foreground"
            />

            {error && <p className="mt-3 text-[12px] text-destructive">{error}</p>}

            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="px-3 h-8 rounded-md text-[12px] font-medium text-muted-foreground hover:text-foreground"
              >
                取消
              </button>
              <button
                type="button"
                onClick={submit}
                disabled={submitting || !name.trim()}
                className="px-3 h-8 rounded-md text-[12px] font-medium bg-primary text-primary-foreground disabled:opacity-50 hover:opacity-90"
              >
                {submitting ? "创建中…" : "创建"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

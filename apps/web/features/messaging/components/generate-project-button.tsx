"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Sparkles } from "lucide-react";
import { toast } from "sonner";

import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { useProjectStore } from "@/features/projects";
import { api } from "@/shared/api";

interface Props {
  sourceType: "channel" | "dm";
  sourceId: string;
  sourceName: string;
  peerType?: "member" | "agent";
}

// GenerateProjectButton lives in the session header. When at least one
// message is selected (via the per-message checkbox in MessageList), the
// button enables and lets the user spin up a Project from that subset
// through POST /api/projects/from-chat. The dialog closes immediately on
// submit so the chat flow isn't blocked while the LLM-driven generation
// runs; progress is reported via toast and the user can jump to the
// project page from the success toast.
export function GenerateProjectButton({ sourceType, sourceId, sourceName, peerType }: Props) {
  const selectedIds = useMessageSelectionStore((s) => s.selectedIds);
  const clearSelection = useMessageSelectionStore((s) => s.clear);
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  // dialogCount snapshots selectedIds.size when the dialog opens so the
  // body copy reflects the real message count even after clearSelection().
  const [dialogCount, setDialogCount] = useState(0);

  const liveCount = selectedIds.size;
  const disabled = liveCount === 0;
  const sourceLabel = sourceType === "channel" ? `#${sourceName}` : sourceName;

  const openDialog = () => {
    if (disabled) return;
    setDialogCount(liveCount);
    setTitle(`Project from ${sourceLabel}`);
    setError(null);
    setOpen(true);
  };

  const submit = () => {
    const trimmed = title.trim();
    if (!trimmed) {
      setError("title is required");
      return;
    }

    const messageIDs = Array.from(selectedIds);
    const body = {
      title: trimmed,
      source_refs: [
        {
          type: sourceType,
          id: sourceId,
          message_ids: messageIDs,
          ...(sourceType === "dm" ? { peer_type: peerType ?? "member" } : {}),
        },
      ],
      agent_ids: [],
      schedule_type: "one_time" as const,
    };

    // Close dialog + clear selection up front so the user's chat flow
    // isn't gated on the LLM call.
    setOpen(false);
    clearSelection();

    const toastId = toast.loading(`正在生成项目 "${trimmed}"…`);

    (async () => {
      try {
        const res = await api.createProjectFromChat(body);
        const projectId =
          (res as { id?: string }).id ??
          (res as { project?: { id?: string } }).project?.id ??
          null;
        const warnings = (res as { warnings?: string[] }).warnings ?? [];

        // Refresh the projects list so the new project shows up wherever
        // the list is mounted (sidebar, /projects, etc).
        void useProjectStore.getState().fetch();

        toast.success("项目已生成", {
          id: toastId,
          description:
            warnings.length > 0
              ? `警告：${warnings.join("; ")}`
              : undefined,
          action: projectId
            ? {
                label: "查看项目",
                onClick: () => router.push(`/projects/${projectId}`),
              }
            : undefined,
        });

        // Auto-jump so the user lands on the project page per product ask,
        // while keeping the chat unblocked during the generation itself.
        if (projectId) {
          router.push(`/projects/${projectId}`);
        }
      } catch (e) {
        toast.error("生成项目失败", {
          id: toastId,
          description: e instanceof Error ? e.message : String(e),
        });
      }
    })();
  };

  return (
    <>
      <button
        type="button"
        onClick={openDialog}
        disabled={disabled}
        title={disabled ? "Select messages first" : `Generate project from ${liveCount} message(s)`}
        className={`flex items-center gap-1 px-2 h-7 rounded-md text-[12px] font-medium transition-colors ${
          disabled
            ? "text-muted-foreground/60 cursor-not-allowed"
            : "bg-primary text-primary-foreground hover:opacity-90"
        }`}
      >
        <Sparkles className="h-3.5 w-3.5" />
        Generate Project
        {liveCount > 0 && (
          <span className="ml-1 text-[10px] px-1 rounded bg-primary-foreground/20">
            {liveCount}
          </span>
        )}
      </button>

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-background rounded-lg shadow-lg w-full max-w-md p-5">
            <h2 className="text-base font-semibold text-foreground">Generate Project from selection</h2>
            <p className="text-[12px] text-muted-foreground mt-1">
              {dialogCount} message(s) from {sourceLabel} will be summarized into a Plan with Tasks.
            </p>

            <label className="block text-[12px] font-medium text-foreground mt-4">Project title</label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
              className="mt-1 w-full px-2 py-1.5 rounded-md border border-border bg-background text-[13px] text-foreground"
            />

            {error && (
              <p className="mt-3 text-[12px] text-destructive">{error}</p>
            )}

            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="px-3 h-8 rounded-md text-[12px] font-medium text-muted-foreground hover:text-foreground"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={submit}
                disabled={!title.trim()}
                className="px-3 h-8 rounded-md text-[12px] font-medium bg-primary text-primary-foreground disabled:opacity-50 hover:opacity-90"
              >
                Generate
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

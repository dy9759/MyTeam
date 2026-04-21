"use client";

import { useState } from "react";
import { Sparkles } from "lucide-react";

import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { api } from "@/shared/api";

interface Props {
  // Conversation source — either a channel or a DM peer.
  sourceType: "channel" | "dm";
  // Channel id or DM peer id.
  sourceId: string;
  // Display name used to seed the dialog title.
  sourceName: string;
  // Required when sourceType === "dm" — the peer's actor type so the
  // backend can query ListDMMessages with the correct recipient_type.
  peerType?: "member" | "agent";
}

// GenerateProjectButton lives in the session header. When at least one
// message is selected (via the per-message checkbox in MessageList), the
// button enables and lets the user spin up a Project from that subset
// through POST /api/projects/from-chat. Works for both channels and DMs.
export function GenerateProjectButton({ sourceType, sourceId, sourceName, peerType }: Props) {
  const selectedIds = useMessageSelectionStore((s) => s.selectedIds);
  const clearSelection = useMessageSelectionStore((s) => s.clear);

  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createdId, setCreatedId] = useState<string | null>(null);
  // dialogCount snapshots selectedIds.size when the dialog opens. Without
  // the snapshot the dialog body would read from the live store, and the
  // clearSelection() call on success would make the count flip to 0 —
  // masking the fact that the project was generated from N real messages.
  const [dialogCount, setDialogCount] = useState(0);
  // PlanGenerator surfaces warnings (e.g. LLM_UNAVAILABLE, PLAN_GEN_MALFORMED)
  // when it had to fall back. Surface them so the user knows the resulting
  // plan needs more attention than a clean LLM-generated one.
  const [warnings, setWarnings] = useState<string[]>([]);

  const liveCount = selectedIds.size;
  const disabled = liveCount === 0;
  const sourceLabel = sourceType === "channel" ? `#${sourceName}` : sourceName;

  const openDialog = () => {
    if (disabled) return;
    setDialogCount(liveCount);
    setTitle(`Project from ${sourceLabel}`);
    setError(null);
    setCreatedId(null);
    setWarnings([]);
    setOpen(true);
  };

  const submit = async () => {
    if (!title.trim()) {
      setError("title is required");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const res = await api.createProjectFromChat({
        title: title.trim(),
        source_refs: [
          {
            type: sourceType,
            id: sourceId,
            message_ids: Array.from(selectedIds),
            ...(sourceType === "dm" ? { peer_type: peerType ?? "member" } : {}),
          },
        ],
        agent_ids: [],
        schedule_type: "one_time",
      });
      // shared/api may return either a bare Project or a wrapper { project }.
      // Normalize so the link works in both cases.
      const projectId =
        (res as { id?: string }).id ??
        (res as { project?: { id?: string } }).project?.id ??
        null;
      const responseWarnings =
        (res as { warnings?: string[] }).warnings ?? [];
      setCreatedId(projectId);
      setWarnings(responseWarnings);
      clearSelection();
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
              disabled={submitting || createdId !== null}
              autoFocus
              className="mt-1 w-full px-2 py-1.5 rounded-md border border-border bg-background text-[13px] text-foreground"
            />

            {error && (
              <p className="mt-3 text-[12px] text-destructive">{error}</p>
            )}

            {createdId && (
              <div className="mt-3 text-[12px] text-foreground bg-primary/10 rounded-md p-2">
                Created. <a href={`/plans/${createdId}`} className="underline">View plan →</a>
              </div>
            )}

            {warnings.length > 0 && (
              <div className="mt-2 text-[12px] text-foreground bg-yellow-500/10 border border-yellow-500/30 rounded-md p-2">
                <div className="font-medium mb-1">Plan generated with warnings:</div>
                <ul className="list-disc pl-4 space-y-0.5">
                  {warnings.map((w) => (
                    <li key={w}>
                      <code className="text-[11px]">{w}</code>
                    </li>
                  ))}
                </ul>
                <div className="mt-1 text-muted-foreground">
                  Review the plan before kicking off a run.
                </div>
              </div>
            )}

            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  setOpen(false);
                }}
                className="px-3 h-8 rounded-md text-[12px] font-medium text-muted-foreground hover:text-foreground"
              >
                {createdId ? "Close" : "Cancel"}
              </button>
              {!createdId && (
                <button
                  type="button"
                  onClick={submit}
                  disabled={submitting || !title.trim()}
                  className="px-3 h-8 rounded-md text-[12px] font-medium bg-primary text-primary-foreground disabled:opacity-50 hover:opacity-90"
                >
                  {submitting ? "Generating…" : "Generate"}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}

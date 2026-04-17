"use client";

import { useEffect } from "react";
import { Trash2 } from "lucide-react";
import { useThreadStore } from "../store";
import type { ThreadContextItemType } from "@/shared/types";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/shared/api";

const TYPE_LABELS: Record<ThreadContextItemType, string> = {
  decision: "Decision",
  file: "File",
  code_snippet: "Code",
  summary: "Summary",
  reference: "Reference",
};

const TYPE_VARIANTS: Record<
  ThreadContextItemType,
  "default" | "secondary" | "outline"
> = {
  decision: "default",
  file: "secondary",
  code_snippet: "secondary",
  summary: "outline",
  reference: "outline",
};

export function ThreadContextPanel({ threadID }: { threadID: string }) {
  const items = useThreadStore((s) => s.contextItemsByThread[threadID] ?? []);
  const loading = useThreadStore((s) => s.loading[threadID] ?? false);
  const loadContextItems = useThreadStore((s) => s.loadContextItems);
  const removeContextItem = useThreadStore((s) => s.removeContextItem);

  useEffect(() => {
    loadContextItems(threadID).catch(() => void 0);
  }, [threadID, loadContextItems]);

  const handleDelete = async (itemID: string) => {
    await api.deleteThreadContextItem(threadID, itemID);
    removeContextItem(threadID, itemID);
  };

  if (loading && items.length === 0) {
    return <p className="text-muted-foreground text-sm">Loading context…</p>;
  }
  if (items.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">No context items yet.</p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {items.map((item) => (
        <Card key={item.id} className="flex flex-row items-start gap-3 p-3">
          <Badge variant={TYPE_VARIANTS[item.item_type]}>
            {TYPE_LABELS[item.item_type]}
          </Badge>
          <div className="min-w-0 flex-1">
            {item.title && (
              <div className="truncate font-medium">{item.title}</div>
            )}
            {item.body && (
              <div className="text-muted-foreground mt-1 text-sm whitespace-pre-wrap">
                {item.body}
              </div>
            )}
            <div className="text-muted-foreground mt-2 text-xs">
              {new Date(item.created_at).toLocaleString()} ·{" "}
              {item.retention_class}
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => handleDelete(item.id)}
            className="text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </Card>
      ))}
    </div>
  );
}

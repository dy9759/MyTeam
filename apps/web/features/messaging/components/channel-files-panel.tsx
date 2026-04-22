"use client";

import { useMemo } from "react";
import { FileText, X } from "lucide-react";

import type { Message } from "@/shared/types/messaging";
import { useWorkspaceStore } from "@/features/workspace";
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store";
import { formatSize, getFileIcon } from "@/shared/file-display";

interface ChannelFilesPanelProps {
  messages: Message[];
  onClose: () => void;
}

export function ChannelFilesPanel({ messages, onClose }: ChannelFilesPanelProps) {
  const members = useWorkspaceStore((s) => s.members);
  const openFile = useFileViewerStore((s) => s.open);

  const files = useMemo(() => {
    return messages
      .filter((m) => m.file_id && m.file_name)
      .slice()
      .sort((a, b) => (b.created_at > a.created_at ? 1 : -1));
  }, [messages]);

  const resolveName = (senderId: string) =>
    members.find((m) => m.user_id === senderId)?.name ?? senderId.slice(0, 8);

  return (
    <div className="w-[360px] border-l border-border flex flex-col h-full bg-card">
      <div className="px-4 py-3 border-b border-border flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <FileText className="h-4 w-4 text-primary shrink-0" />
          <h3 className="font-medium text-[14px] text-foreground">频道文件</h3>
          <span className="text-[12px] text-muted-foreground">{files.length}</span>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
          title="关闭"
          aria-label="关闭"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="flex-1 overflow-auto">
        {files.length === 0 ? (
          <div className="p-6 text-[13px] text-muted-foreground text-center">
            当前频道还没有上传的文件。
          </div>
        ) : (
          <ul className="p-2 space-y-1">
            {files.map((msg) => (
              <li key={msg.id}>
                <button
                  type="button"
                  onClick={() =>
                    openFile({
                      file_id: msg.file_id!,
                      file_name: msg.file_name!,
                      file_size: msg.file_size,
                      file_content_type: msg.file_content_type,
                    })
                  }
                  className="w-full text-left rounded-md px-2 py-2 hover:bg-accent/50 transition-colors flex items-start gap-2"
                  title="打开文件预览"
                >
                  <span className="text-lg leading-none shrink-0 mt-0.5">{getFileIcon(msg.file_name!)}</span>
                  <div className="min-w-0 flex-1">
                    <div className="text-[13px] text-foreground truncate">{msg.file_name}</div>
                    <div className="text-[11px] text-muted-foreground truncate">
                      {resolveName(msg.sender_id)} · {new Date(msg.created_at).toLocaleString()}
                      {msg.file_size != null && ` · ${formatSize(msg.file_size)}`}
                    </div>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

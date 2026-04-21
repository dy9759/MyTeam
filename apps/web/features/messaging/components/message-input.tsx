"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { FileIcon, Loader2, Paperclip, X } from "lucide-react";
import { detectTrigger, filterCandidates } from "@myteam/client-core";
import { api } from "@/shared/api";
import { toast } from "sonner";
import { useWorkspaceStore } from "@/features/workspace";
import { useAuthStore } from "@/features/auth";
import { MentionPicker, type MentionCandidate } from "./mention-picker";

interface AttachmentPreview {
  file: File;
  name: string;
  size: string;
}

interface MessageInputProps {
  onSend: (content: string, fileInfo?: { file_id: string; file_name: string; file_size: number; file_content_type: string }) => Promise<void>;
  placeholder?: string;
  disabled?: boolean;
  onTyping?: (isTyping: boolean) => void;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const STOP_TYPING_DELAY_MS = 2000;

export function MessageInput({
  onSend,
  placeholder = "输入消息...",
  disabled,
  onTyping,
}: MessageInputProps) {
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [attachments, setAttachments] = useState<AttachmentPreview[]>([]);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const stopTypingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Mention picker state — trigger detection is pure; pickerOpen gates rendering.
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerQuery, setPickerQuery] = useState("");
  const [pickerRange, setPickerRange] = useState<{ start: number; end: number }>({
    start: 0,
    end: 0,
  });

  // Candidate source: workspace members + agents (minus self). Filtered
  // inside MentionPicker via @myteam/client-core.filterCandidates.
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const currentUserId = useAuthStore((s) => s.user?.id);
  const candidates = useMemo<MentionCandidate[]>(() => {
    const list: MentionCandidate[] = [];
    for (const m of members) {
      if (m.user_id === currentUserId) continue;
      const name = m.name || m.email || m.user_id;
      list.push({ id: m.user_id, name, kind: "owner" });
    }
    for (const a of agents) {
      list.push({ id: a.id, name: a.display_name || a.name, kind: "agent" });
    }
    return list;
  }, [members, agents, currentUserId]);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (stopTypingTimerRef.current) {
        clearTimeout(stopTypingTimerRef.current);
      }
    };
  }, []);

  const recomputeTrigger = useCallback(
    (text: string, pos: number) => {
      const t = detectTrigger(text, pos);
      const hasMatches =
        t.triggering && filterCandidates(candidates, t.query).length > 0;
      setPickerOpen(hasMatches);
      setPickerQuery(t.query);
      if (t.triggering) setPickerRange({ start: t.start, end: t.end });
    },
    [candidates],
  );

  function handleFileSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files;
    if (!files) return;
    const newAttachments: AttachmentPreview[] = [];
    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      if (!f) continue;
      newAttachments.push({ file: f, name: f.name, size: formatSize(f.size) });
    }
    setAttachments((prev) => [...prev, ...newAttachments]);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function removeAttachment(index: number) {
    setAttachments((prev) => prev.filter((_, i) => i !== index));
  }

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      const next = e.target.value;
      const pos = e.target.selectionStart ?? next.length;
      setInput(next);
      recomputeTrigger(next, pos);

      if (onTyping) {
        onTyping(true);
        if (stopTypingTimerRef.current) {
          clearTimeout(stopTypingTimerRef.current);
        }
        stopTypingTimerRef.current = setTimeout(() => {
          onTyping(false);
          stopTypingTimerRef.current = null;
        }, STOP_TYPING_DELAY_MS);
      }
    },
    [onTyping, recomputeTrigger],
  );

  const handleSelect = useCallback(
    (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
      const target = e.currentTarget;
      const pos = target.selectionStart ?? input.length;
      recomputeTrigger(input, pos);
    },
    [input, recomputeTrigger],
  );

  const handleBlur = useCallback(() => {
    if (onTyping) {
      onTyping(false);
      if (stopTypingTimerRef.current) {
        clearTimeout(stopTypingTimerRef.current);
        stopTypingTimerRef.current = null;
      }
    }
  }, [onTyping]);

  const insertMention = useCallback(
    (c: MentionCandidate) => {
      const before = input.slice(0, pickerRange.start);
      const after = input.slice(pickerRange.end);
      const inserted = `@${c.name} `;
      const next = before + inserted + after;
      setInput(next);
      setPickerOpen(false);
      const newPos = (before + inserted).length;
      requestAnimationFrame(() => {
        textareaRef.current?.focus();
        textareaRef.current?.setSelectionRange(newPos, newPos);
      });
    },
    [input, pickerRange],
  );

  async function doSubmit() {
    if ((!input.trim() && attachments.length === 0) || sending) return;

    if (onTyping) {
      onTyping(false);
      if (stopTypingTimerRef.current) {
        clearTimeout(stopTypingTimerRef.current);
        stopTypingTimerRef.current = null;
      }
    }

    setSending(true);

    try {
      if (attachments.length > 0) {
        setUploading(true);
        for (const att of attachments) {
          try {
            const uploaded = await api.uploadFile(att.file);
            await onSend(input.trim() || att.name, {
              file_id: uploaded.id,
              file_name: att.name,
              file_size: att.file.size,
              file_content_type: att.file.type || "application/octet-stream",
            });
          } catch (err) {
            const msg = err instanceof Error ? err.message : "";
            if (msg.includes("503") || msg.includes("not configured") || msg.includes("unavailable")) {
              toast.error("文件上传服务未配置(需要 S3 存储)");
            } else {
              toast.error(`上传 ${att.name} 失败`);
            }
            if (input.trim()) {
              await onSend(input.trim());
            }
          }
        }
        setUploading(false);
      } else {
        await onSend(input.trim());
      }
      setInput("");
      setAttachments([]);
      setPickerOpen(false);
    } finally {
      setSending(false);
      setUploading(false);
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    await doSubmit();
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // When the picker is open, let it handle navigation keys.
    if (pickerOpen && ["ArrowUp", "ArrowDown", "Enter", "Escape"].includes(e.key)) {
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void doSubmit();
    }
  };

  return (
    <div className="border-t">
      {/* Attachment previews */}
      {attachments.length > 0 && (
        <div className="flex flex-wrap gap-2 px-4 pt-3">
          {attachments.map((att, i) => (
            <div
              key={`${att.name}-${i}`}
              className="flex items-center gap-2 rounded-md border bg-muted/50 px-3 py-1.5 text-sm"
            >
              <FileIcon className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="max-w-[200px] truncate">{att.name}</span>
              <span className="text-xs text-muted-foreground">{att.size}</span>
              <button
                type="button"
                onClick={() => removeAttachment(i)}
                className="ml-1 rounded-full p-0.5 hover:bg-muted"
              >
                <X className="h-3 w-3 text-muted-foreground" />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Input row */}
      <form onSubmit={handleSubmit} className="flex items-end gap-2 p-4">
        {/* File upload button */}
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={disabled || uploading}
          className="shrink-0 rounded-md p-2 hover:bg-muted transition-colors text-muted-foreground hover:text-foreground disabled:opacity-50"
          title="添加附件"
        >
          <Paperclip className="h-4 w-4" />
        </button>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={handleFileSelect}
        />

        {/* Textarea + mention picker */}
        <div className="relative flex-1">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            onSelect={handleSelect}
            onBlur={handleBlur}
            rows={1}
            className="w-full resize-none px-4 py-2 bg-muted border rounded-md text-sm leading-6 max-h-32 overflow-auto"
            placeholder={placeholder}
            disabled={disabled}
          />
          {pickerOpen && (
            <MentionPicker
              candidates={candidates}
              query={pickerQuery}
              onSelect={insertMention}
              onClose={() => setPickerOpen(false)}
            />
          )}
        </div>

        {/* Send button */}
        <button
          type="submit"
          disabled={sending || (!input.trim() && attachments.length === 0) || disabled}
          className="shrink-0 px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
        >
          {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : "发送"}
        </button>
      </form>
    </div>
  );
}

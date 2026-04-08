"use client";
import { useState, useRef } from "react";
import { Paperclip, X, FileIcon, Loader2 } from "lucide-react";
import { api } from "@/shared/api";

interface AttachmentPreview {
  file: File;
  name: string;
  size: string;
}

interface MessageInputProps {
  onSend: (content: string, fileInfo?: { file_id: string; file_name: string; file_size: number; file_content_type: string }) => Promise<void>;
  placeholder?: string;
  disabled?: boolean;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function MessageInput({
  onSend,
  placeholder = "输入消息...",
  disabled,
}: MessageInputProps) {
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [attachments, setAttachments] = useState<AttachmentPreview[]>([]);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  function handleFileSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files;
    if (!files) return;
    const newAttachments: AttachmentPreview[] = [];
    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      newAttachments.push({ file: f, name: f.name, size: formatSize(f.size) });
    }
    setAttachments((prev) => [...prev, ...newAttachments]);
    // Reset input so same file can be selected again
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function removeAttachment(index: number) {
    setAttachments((prev) => prev.filter((_, i) => i !== index));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if ((!input.trim() && attachments.length === 0) || sending) return;
    setSending(true);

    try {
      // Upload attachments first, then send message for each
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
          } catch {
            // If upload fails, send text-only message
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
    } finally {
      setSending(false);
      setUploading(false);
    }
  }

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
      <form onSubmit={handleSubmit} className="flex items-center gap-2 p-4">
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

        {/* Text input */}
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          className="flex-1 px-4 py-2 bg-muted border rounded-md text-sm"
          placeholder={placeholder}
          disabled={disabled}
        />

        {/* Send button */}
        <button
          type="submit"
          disabled={sending || (!input.trim() && attachments.length === 0) || disabled}
          className="shrink-0 px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
        >
          {uploading ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            "发送"
          )}
        </button>
      </form>
    </div>
  );
}

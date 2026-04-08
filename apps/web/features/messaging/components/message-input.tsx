"use client";
import { useState } from "react";

interface MessageInputProps {
  onSend: (content: string) => Promise<void>;
  placeholder?: string;
  disabled?: boolean;
}

export function MessageInput({
  onSend,
  placeholder = "输入消息...",
  disabled,
}: MessageInputProps) {
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!input.trim() || sending) return;
    setSending(true);
    try {
      await onSend(input.trim());
      setInput("");
    } finally {
      setSending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex gap-2 p-4 border-t">
      <input
        value={input}
        onChange={(e) => setInput(e.target.value)}
        className="flex-1 px-4 py-2 bg-muted border rounded-md text-sm"
        placeholder={placeholder}
        disabled={disabled}
      />
      <button
        type="submit"
        disabled={sending || !input.trim() || disabled}
        className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
      >
        发送
      </button>
    </form>
  );
}

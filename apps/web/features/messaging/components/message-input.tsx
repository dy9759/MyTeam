"use client";
import { useState, useRef, useCallback, useEffect } from "react";

interface MessageInputProps {
  onSend: (content: string) => Promise<void>;
  placeholder?: string;
  disabled?: boolean;
  onTyping?: (isTyping: boolean) => void;
}

const STOP_TYPING_DELAY_MS = 2000;

export function MessageInput({
  onSend,
  placeholder = "Type a message...",
  disabled,
  onTyping,
}: MessageInputProps) {
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const stopTypingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (stopTypingTimerRef.current) {
        clearTimeout(stopTypingTimerRef.current);
      }
    };
  }, []);

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setInput(e.target.value);

      if (onTyping) {
        onTyping(true);

        // Reset the stop-typing timer
        if (stopTypingTimerRef.current) {
          clearTimeout(stopTypingTimerRef.current);
        }
        stopTypingTimerRef.current = setTimeout(() => {
          onTyping(false);
          stopTypingTimerRef.current = null;
        }, STOP_TYPING_DELAY_MS);
      }
    },
    [onTyping],
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

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!input.trim() || sending) return;

    // Stop typing indicator on send
    if (onTyping) {
      onTyping(false);
      if (stopTypingTimerRef.current) {
        clearTimeout(stopTypingTimerRef.current);
        stopTypingTimerRef.current = null;
      }
    }

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
        onChange={handleChange}
        onBlur={handleBlur}
        className="flex-1 px-4 py-2 bg-muted border rounded-md text-sm"
        placeholder={placeholder}
        disabled={disabled}
      />
      <button
        type="submit"
        disabled={sending || !input.trim() || disabled}
        className="px-4 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium disabled:opacity-50"
      >
        Send
      </button>
    </form>
  );
}

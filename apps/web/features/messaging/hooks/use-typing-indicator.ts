"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { useWSEvent } from "@/features/realtime";
import { useAuthStore } from "@/features/auth";
import { api } from "@/shared/api";
import type { TypingPayload } from "@/shared/types/events";

const TYPING_TIMEOUT_MS = 3000;
const SEND_DEBOUNCE_MS = 1000;

interface UseTypingIndicatorParams {
  channelId?: string;
}

interface UseTypingIndicatorReturn {
  typingUsers: string[];
  sendTyping: (isTyping: boolean) => void;
}

export function useTypingIndicator({
  channelId,
}: UseTypingIndicatorParams): UseTypingIndicatorReturn {
  const [typingUsers, setTypingUsers] = useState<string[]>([]);
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const lastSentRef = useRef<number>(0);
  const sendTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clean up timers on unmount
  useEffect(() => {
    return () => {
      for (const timer of timersRef.current.values()) {
        clearTimeout(timer);
      }
      timersRef.current.clear();
      if (sendTimeoutRef.current) {
        clearTimeout(sendTimeoutRef.current);
      }
    };
  }, []);

  // Listen for typing events via WebSocket
  useWSEvent(
    "typing",
    useCallback(
      (payload: unknown) => {
        const data = payload as TypingPayload;

        // Filter out self-events
        const selfId = useAuthStore.getState().user?.id;
        if (data.sender_id === selfId) return;

        // Filter by channel
        if (channelId && data.channel_id !== channelId) return;
        if (!channelId) return;

        const senderId = data.sender_id;

        if (data.is_typing) {
          // Clear existing timeout for this user
          const existing = timersRef.current.get(senderId);
          if (existing) clearTimeout(existing);

          // Set auto-clear timeout
          const timer = setTimeout(() => {
            timersRef.current.delete(senderId);
            setTypingUsers((prev) => prev.filter((id) => id !== senderId));
          }, TYPING_TIMEOUT_MS);
          timersRef.current.set(senderId, timer);

          // Add user if not already in the list
          setTypingUsers((prev) =>
            prev.includes(senderId) ? prev : [...prev, senderId],
          );
        } else {
          // User stopped typing
          const existing = timersRef.current.get(senderId);
          if (existing) clearTimeout(existing);
          timersRef.current.delete(senderId);
          setTypingUsers((prev) => prev.filter((id) => id !== senderId));
        }
      },
      [channelId],
    ),
  );

  // Debounced sendTyping
  const sendTyping = useCallback(
    (isTyping: boolean) => {
      const now = Date.now();

      if (isTyping) {
        // Debounce: only send once per second
        if (now - lastSentRef.current < SEND_DEBOUNCE_MS) return;
        lastSentRef.current = now;
      }

      // Clear any pending stop-typing timeout
      if (sendTimeoutRef.current) {
        clearTimeout(sendTimeoutRef.current);
        sendTimeoutRef.current = null;
      }

      api.sendTyping({ channel_id: channelId, is_typing: isTyping }).catch(() => {
        // Silently ignore typing errors
      });
    },
    [channelId],
  );

  return { typingUsers, sendTyping };
}

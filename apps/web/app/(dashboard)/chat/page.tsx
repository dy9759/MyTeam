"use client";

import { useEffect, useRef, useState } from "react";
import { useMessagingStore } from "@/features/messaging/store";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import type { Conversation } from "@/shared/types/messaging";

export default function ChatPage() {
  const { conversations, currentMessages, loading, fetch, fetchMessages, sendMessage } = useMessagingStore();
  const [selected, setSelected] = useState<Conversation | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    fetch();
  }, [fetch]);

  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (!selected) return;
    const params = { recipient_id: selected.peer_id };
    fetchMessages(params);
    pollRef.current = setInterval(() => fetchMessages(params), 3000);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [selected, fetchMessages]);

  function handleSelect(conv: Conversation) {
    setSelected(conv);
  }

  return (
    <div className="flex h-full">
      {/* Conversation list sidebar */}
      <div className="w-72 border-r flex flex-col">
        <div className="p-4 border-b">
          <h2 className="font-semibold">Messages</h2>
        </div>
        <div className="flex-1 overflow-auto">
          {loading && conversations.length === 0 ? (
            <div className="p-4 text-sm text-muted-foreground text-center">Loading...</div>
          ) : conversations.length === 0 ? (
            <div className="text-sm text-muted-foreground p-4 text-center">
              No conversations yet. Send a message to an agent or team member to get started.
            </div>
          ) : (
            conversations.map((conv) => (
              <button
                key={conv.peer_id}
                onClick={() => handleSelect(conv)}
                className={`w-full text-left p-3 hover:bg-muted/50 border-b transition-colors ${
                  selected?.peer_id === conv.peer_id ? "bg-muted" : ""
                }`}
              >
                <div className="flex items-center justify-between">
                  <span className="font-medium text-sm truncate">
                    {conv.peer_name || conv.peer_id.slice(0, 12)}
                  </span>
                  {(conv.unread_count ?? 0) > 0 && (
                    <span className="ml-2 text-xs bg-primary text-primary-foreground rounded-full px-1.5 py-0.5">
                      {conv.unread_count}
                    </span>
                  )}
                </div>
                {conv.last_message && (
                  <div className="text-xs text-muted-foreground mt-1 truncate">
                    {conv.last_message.content}
                  </div>
                )}
              </button>
            ))
          )}
        </div>
      </div>

      {/* Message view */}
      <div className="flex-1 flex flex-col">
        {selected ? (
          <>
            <div className="p-4 border-b">
              <h3 className="font-medium">
                {selected.peer_name || selected.peer_id.slice(0, 12)}
              </h3>
              <span className="text-xs text-muted-foreground">{selected.peer_type}</span>
            </div>
            <MessageList messages={currentMessages} />
            <MessageInput
              onSend={async (content) => {
                await sendMessage({
                  recipient_id: selected.peer_id,
                  recipient_type: selected.peer_type,
                  content,
                });
              }}
            />
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center text-muted-foreground">
            Select a conversation to start messaging
          </div>
        )}
      </div>
    </div>
  );
}

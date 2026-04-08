"use client";

import { useEffect, useRef, useState } from "react";
import { useMessagingStore } from "@/features/messaging/store";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import type { Conversation } from "@/shared/types/messaging";

type ChatTab = "my" | "agent";

export default function ChatPage() {
  const {
    conversations,
    ownerAgentConversations,
    currentMessages,
    loading,
    fetch,
    fetchMessages,
    sendMessage,
    fetchOwnerAgentConversations,
  } = useMessagingStore();
  const [selected, setSelected] = useState<Conversation | null>(null);
  const [chatTab, setChatTab] = useState<ChatTab>("my");
  const [fileTab, setFileTab] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    fetch();
  }, [fetch]);

  useEffect(() => {
    if (chatTab === "agent") {
      fetchOwnerAgentConversations();
    }
  }, [chatTab, fetchOwnerAgentConversations]);

  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (!selected) return;
    const params = { recipient_id: selected.peer_id };
    fetchMessages(params);
    pollRef.current = setInterval(() => fetchMessages(params), 3000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [selected, fetchMessages]);

  function handleSelect(conv: Conversation) {
    setSelected(conv);
    setFileTab(false);
  }

  const displayConversations = chatTab === "agent" ? ownerAgentConversations : conversations;
  const fileMessages = currentMessages.filter(
    (m) => m.file_name || m.content_type === "file",
  );

  return (
    <div className="flex h-full">
      {/* Conversation list sidebar */}
      <div className="w-72 border-r flex flex-col">
        <div className="p-4 border-b space-y-3">
          <h2 className="font-semibold">Messages</h2>
          {/* Tab toggle */}
          <div className="flex gap-1 bg-muted rounded-md p-0.5">
            <button
              onClick={() => { setChatTab("my"); setSelected(null); }}
              className={`flex-1 px-3 py-1 text-xs font-medium rounded transition-colors ${
                chatTab === "my"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              My Chats
            </button>
            <button
              onClick={() => { setChatTab("agent"); setSelected(null); }}
              className={`flex-1 px-3 py-1 text-xs font-medium rounded transition-colors ${
                chatTab === "agent"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              Agent Chats
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-auto">
          {loading && displayConversations.length === 0 ? (
            <div className="p-4 text-sm text-muted-foreground text-center">Loading...</div>
          ) : displayConversations.length === 0 ? (
            <div className="text-sm text-muted-foreground p-4 text-center">
              {chatTab === "agent"
                ? "No agent conversations yet."
                : "No conversations yet. Send a message to an agent or team member to get started."}
            </div>
          ) : (
            displayConversations.map((conv) => (
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
            <div className="p-4 border-b flex items-center justify-between">
              <div>
                <h3 className="font-medium">
                  {selected.peer_name || selected.peer_id.slice(0, 12)}
                </h3>
                <span className="text-xs text-muted-foreground">{selected.peer_type}</span>
              </div>
              <div className="flex gap-1 bg-muted rounded-md p-0.5">
                <button
                  onClick={() => setFileTab(false)}
                  className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                    !fileTab
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground"
                  }`}
                >
                  Messages
                </button>
                <button
                  onClick={() => setFileTab(true)}
                  className={`px-3 py-1 text-xs font-medium rounded transition-colors ${
                    fileTab
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground"
                  }`}
                >
                  Files
                </button>
              </div>
            </div>

            {fileTab ? (
              <div className="flex-1 overflow-auto p-4 space-y-2">
                {fileMessages.length === 0 ? (
                  <div className="text-center text-muted-foreground py-8">
                    No files shared in this conversation
                  </div>
                ) : (
                  fileMessages.map((m) => (
                    <div
                      key={m.id}
                      className="flex items-center gap-3 p-3 border rounded-lg"
                    >
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-sm truncate">
                          {m.file_name ?? "File"}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {new Date(m.created_at).toLocaleString()}
                        </div>
                      </div>
                    </div>
                  ))
                )}
              </div>
            ) : (
              <>
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
            )}
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

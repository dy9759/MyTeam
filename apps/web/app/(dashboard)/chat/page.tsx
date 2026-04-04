"use client";
import { useState } from "react";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";

export default function ChatPage() {
  const [selectedConversation, setSelectedConversation] = useState<string | null>(null);

  return (
    <div className="flex h-full">
      {/* Conversation list sidebar */}
      <div className="w-64 border-r flex flex-col">
        <div className="p-4 border-b">
          <h2 className="font-semibold">Messages</h2>
        </div>
        <div className="flex-1 overflow-auto p-2">
          <div className="text-sm text-muted-foreground p-4 text-center">
            No conversations yet. Send a message to an agent or team member to get started.
          </div>
        </div>
      </div>

      {/* Message view */}
      <div className="flex-1 flex flex-col">
        {selectedConversation ? (
          <>
            <div className="p-4 border-b">
              <h3 className="font-medium">{selectedConversation}</h3>
            </div>
            <MessageList messages={[]} />
            <MessageInput onSend={async (content) => { console.log("send:", content); }} />
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

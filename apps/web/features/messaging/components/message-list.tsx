"use client";

import { useWorkspaceStore } from "@/features/workspace";

interface MessageListProps {
  messages: Array<{
    id: string;
    sender_id: string;
    sender_type: string;
    content: string;
    created_at: string;
    file_name?: string;
    file_id?: string;
  }>;
  currentUserId?: string;
  typingUsers?: string[];
}

export function MessageList({ messages, currentUserId, typingUsers = [] }: MessageListProps) {
  const members = useWorkspaceStore((s) => s.members);

  const resolveDisplayName = (senderId: string): string => {
    const member = members.find((m) => m.user_id === senderId);
    return member?.name ?? senderId.slice(0, 12);
  };

  return (
    <div className="flex-1 overflow-auto p-4 space-y-3">
      {messages.map((msg) => (
        <div
          key={msg.id}
          className={`flex ${msg.sender_id === currentUserId ? "justify-end" : "justify-start"}`}
        >
          <div
            className={`max-w-[70%] px-4 py-2 rounded-lg text-sm ${
              msg.sender_id === currentUserId
                ? "bg-primary text-primary-foreground"
                : "bg-muted"
            }`}
          >
            <div className="text-xs opacity-70 mb-1">
              {resolveDisplayName(msg.sender_id)} &middot;{" "}
              {new Date(msg.created_at).toLocaleTimeString()}
            </div>
            <div>{msg.content}</div>
            {msg.file_name && (
              <div className="mt-1 flex items-center gap-1 text-xs">
                {msg.file_name}
              </div>
            )}
          </div>
        </div>
      ))}
      {messages.length === 0 && (
        <div className="text-center text-muted-foreground mt-8">
          No messages yet
        </div>
      )}
      {typingUsers.length > 0 && (
        <TypingIndicator typingUsers={typingUsers} resolveDisplayName={resolveDisplayName} />
      )}
    </div>
  );
}

function TypingIndicator({
  typingUsers,
  resolveDisplayName,
}: {
  typingUsers: string[];
  resolveDisplayName: (id: string) => string;
}) {
  const names = typingUsers.map(resolveDisplayName);
  let label: string;
  if (names.length === 1) {
    label = `${names[0]} is typing`;
  } else if (names.length === 2) {
    label = `${names[0]} and ${names[1]} are typing`;
  } else {
    label = `${names[0]} and ${names.length - 1} others are typing`;
  }

  return (
    <div className="flex items-center gap-1.5 px-4 py-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="inline-flex gap-0.5">
        <span className="animate-bounce [animation-delay:0ms]">&middot;</span>
        <span className="animate-bounce [animation-delay:150ms]">&middot;</span>
        <span className="animate-bounce [animation-delay:300ms]">&middot;</span>
      </span>
    </div>
  );
}

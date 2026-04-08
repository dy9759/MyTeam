"use client";

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
}

export function MessageList({ messages, currentUserId }: MessageListProps) {
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
              {msg.sender_id.slice(0, 12)} &middot;{" "}
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
          暂无消息
        </div>
      )}
    </div>
  );
}

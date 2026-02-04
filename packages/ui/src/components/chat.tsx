"use client";

import { useRef, useCallback } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { ChatInput } from "@multica/ui/components/chat-input";
import { useConnectionStore, useMessagesStore, useAutoConnect } from "@multica/store";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { ConnectPrompt } from "./connect-prompt";
import { MessageList } from "./message-list";
import { ChatSkeleton } from "./chat-skeleton";

export function Chat() {
  const { loading } = useAutoConnect()

  const agentId = useConnectionStore((s) => s.agentId)
  const gwState = useConnectionStore((s) => s.connectionState)
  const hubId = useConnectionStore((s) => s.hubId)
  const lastError = useConnectionStore((s) => s.lastError)

  const messages = useMessagesStore((s) => s.messages)
  const streamingIds = useMessagesStore((s) => s.streamingIds)

  const isConnected = gwState === "registered" && !!hubId && !!agentId

  const handleSend = useCallback((text: string) => {
    const { hubId, agentId, send, connectionState } = useConnectionStore.getState()
    if (connectionState !== "registered" || !hubId || !agentId) return
    useMessagesStore.getState().sendMessage(text, { hubId, agentId, send })
  }, [])

  const handleDisconnect = useCallback(() => {
    useConnectionStore.getState().disconnect()
  }, [])

  const mainRef = useRef<HTMLElement>(null)
  const fadeStyle = useScrollFade(mainRef)
  useAutoScroll(mainRef)

  return (
    <div className="h-full flex flex-col overflow-hidden w-full">
      {isConnected && (
        <div className="flex items-center justify-end px-4 py-1 max-w-4xl mx-auto w-full">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleDisconnect}
            className="text-xs text-muted-foreground"
          >
            Disconnect
          </Button>
        </div>
      )}

      <main ref={mainRef} className="flex-1 overflow-y-auto min-h-0" style={fadeStyle}>
        {loading ? (
          <ChatSkeleton />
        ) : !isConnected ? (
          <ConnectPrompt />
        ) : messages.length === 0 ? (
          <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
            Send a message to start the conversation
          </div>
        ) : (
          <MessageList messages={messages} streamingIds={streamingIds} />
        )}
      </main>

      {/* Error banner */}
      {lastError && (
        <div className="px-4 py-2 max-w-4xl mx-auto w-full" role="alert" aria-live="polite">
          <div className="rounded-md bg-destructive/10 text-destructive text-sm px-3 py-2 flex items-center justify-between">
            <span>{lastError.message} ({lastError.code})</span>
            <button
              type="button"
              aria-label="Dismiss error"
              onClick={() => useConnectionStore.setState({ lastError: null })}
              className="text-destructive/60 hover:text-destructive ml-2 text-xs focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 rounded outline-none"
            >
              &times;
            </button>
          </div>
        </div>
      )}

      {/* Footer */}
      <footer className="w-full p-2 pt-1 max-w-4xl mx-auto">
        <ChatInput
          onSubmit={handleSend}
          disabled={!isConnected}
          placeholder={!isConnected ? "Connect first..." : "Type a message..."}
        />
      </footer>
    </div>
  );
}

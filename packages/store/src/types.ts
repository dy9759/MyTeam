import type { ContentBlock } from "@multica/sdk"

export type ToolStatus = "running" | "success" | "error" | "interrupted"

export interface Message {
  id: string
  role: "user" | "assistant" | "toolResult"
  content: ContentBlock[]
  agentId: string
  stopReason?: string
  toolCallId?: string
  toolName?: string
  toolArgs?: Record<string, unknown>
  toolStatus?: ToolStatus
  isError?: boolean
}

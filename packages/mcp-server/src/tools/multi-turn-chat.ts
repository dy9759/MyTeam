import { z } from "zod";
import type { UnifiedClient } from "../client/unified-client.js";

export function registerMultiTurnChatTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  client: UnifiedClient,
  state: { agentId?: string; ownerId?: string },
) {
  server.registerTool(
    "agentmesh_multi_turn_chat",
    {
      description:
        "Post a turn to a thread and return the latest thread state. " +
        "If threadId is omitted, creates a new thread in the given channel and posts the first message. " +
        "Each call is one turn — call again with the same threadId to continue the conversation. " +
        "Unlike the old session model, threads are channel-scoped: all channel members can participate.",
      inputSchema: {
        threadId: z.string().optional().describe("Existing thread ID to post to. If omitted, a new thread is created in `channelId`."),
        channelId: z.string().optional().describe("Channel ID to create the thread in (required when threadId is omitted)"),
        title: z.string().optional().describe("Thread title (only used when creating a new thread)"),
        message: z.string().describe("Your message for this turn"),
        issueId: z.string().optional().describe("Optional issue to link (only used when creating a new thread)"),
        rootMessageId: z.string().optional().describe("Optional channel message to root this thread under (only used when creating)"),
        limit: z.number().int().min(1).max(100).optional().describe("Max recent messages to return after posting (default 20)"),
      },
    },
    async ({ threadId, channelId, title, message, issueId, rootMessageId, limit }) => {
      const myId = state.agentId ?? state.ownerId;
      if (!myId) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: "Not registered. Call agentmesh_register first." }) }],
          isError: true,
        };
      }

      let currentThreadId = threadId ?? "";

      // Step 1: Create thread if needed
      if (currentThreadId === "") {
        if (!channelId) {
          return {
            content: [{
              type: "text" as const,
              text: JSON.stringify({
                error: "When threadId is omitted, channelId is required to create a new thread.",
              }),
            }],
            isError: true,
          };
        }
        try {
          const thread = await client.createThread({
            channel_id: channelId,
            title: title ?? "Thread",
            root_message_id: rootMessageId,
            issue_id: issueId,
          });
          currentThreadId = thread?.id;
          if (!currentThreadId) {
            return {
              content: [{ type: "text" as const, text: JSON.stringify({ error: "Thread created but no id returned", raw: thread }) }],
              isError: true,
            };
          }
        } catch (err: any) {
          return {
            content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to create thread: ${err.message ?? err}` }) }],
            isError: true,
          };
        }
      }

      // Step 2: Post this turn's message
      try {
        await client.postThreadMessage(currentThreadId, { content: message });
      } catch (err: any) {
        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              error: `Failed to post message: ${err.message ?? err}`,
              threadId: currentThreadId,
            }),
          }],
          isError: true,
        };
      }

      // Step 3: Fetch updated thread state + recent messages
      let thread: any = undefined;
      let messages: any[] = [];
      try {
        thread = await client.getThread(currentThreadId);
      } catch { /* non-fatal */ }
      try {
        const listResp = await client.listThreadMessages(currentThreadId, { limit: limit ?? 20 });
        messages = Array.isArray(listResp)
          ? listResp
          : (listResp?.messages ?? listResp?.items ?? []);
      } catch { /* non-fatal */ }

      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({
            threadId: currentThreadId,
            sessionId: currentThreadId,
            channelId: thread?.channel_id ?? channelId,
            title: thread?.title,
            status: thread?.status,
            replyCount: thread?.reply_count,
            lastReplyAt: thread?.last_reply_at,
            messages,
            hint: "Call agentmesh_multi_turn_chat again with this threadId to continue the conversation.",
          }, null, 2),
        }],
      };
    },
  );
}

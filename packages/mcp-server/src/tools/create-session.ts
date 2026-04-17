import { z } from "zod";
import type { UnifiedClient } from "../client/unified-client.js";

export function registerCreateSessionTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  client: UnifiedClient,
  state: { agentId?: string; ownerId?: string },
) {
  server.registerTool(
    "agentmesh_create_session",
    {
      description:
        "Create a thread in a channel to collaborate on a specific topic. " +
        "Threads track conversation history and shared context. " +
        "Use this to start a structured discussion on a specific topic within a channel.",
      inputSchema: {
        channelId: z.string().describe("The channel to create the thread in"),
        title: z.string().describe("Topic/title for the thread"),
        rootMessageId: z
          .string()
          .optional()
          .describe("Optional channel message to root this thread under"),
        issueId: z.string().optional().describe("Optional issue to link the thread to"),
      },
    },
    async ({ channelId, title, rootMessageId, issueId }) => {
      const myId = state.agentId ?? state.ownerId;
      if (!myId) {
        return { content: [{ type: "text" as const, text: JSON.stringify({ error: "Not registered. Call agentmesh_register first." }) }], isError: true };
      }

      try {
        const result = await client.createThread({
          channel_id: channelId,
          title,
          root_message_id: rootMessageId,
          issue_id: issueId,
        });

        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              sessionId: result.id,
              threadId: result.id,
              channelId: result.channel_id,
              title: result.title,
              status: result.status,
              message: `Thread '${title}' created. Use agentmesh_multi_turn_chat to start the conversation.`,
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to create thread: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );
}

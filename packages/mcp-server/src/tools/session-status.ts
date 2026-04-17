import { z } from "zod";
import type { HubClient } from "../client/hub-client.js";

export function registerSessionStatusTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  client: HubClient,
) {
  server.registerTool(
    "agentmesh_session_status",
    {
      description:
        "Get the status and details of a thread, including title, status, reply count, and last reply time.",
      inputSchema: {
        threadId: z.string().describe("The thread ID to check"),
      },
    },
    async ({ threadId }) => {
      try {
        const thread = await client.getThread(threadId);
        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              threadId,
              sessionId: threadId,
              channelId: thread?.channel_id,
              title: thread?.title,
              status: thread?.status,
              replyCount: thread?.reply_count,
              lastReplyAt: thread?.last_reply_at,
              rootMessageId: thread?.root_message_id,
              issueId: thread?.issue_id,
              createdAt: thread?.created_at,
              raw: thread,
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to get thread: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );

  server.registerTool(
    "agentmesh_list_sessions",
    {
      description:
        "List threads for a channel. Currently disabled during the session-to-thread migration — " +
        "use agentmesh_session_status with a specific threadId instead.",
      inputSchema: {
        channelId: z.string().optional().describe("Channel ID to list threads for"),
      },
    },
    async () => {
      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({
            error: "agentmesh_list_sessions is temporarily disabled during thread migration. " +
              "Use agentmesh_session_status with a specific threadId to inspect a thread.",
          }),
        }],
        isError: true,
      };
    },
  );
}

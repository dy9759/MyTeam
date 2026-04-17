import { z } from "zod";
import type { HubClient } from "../client/hub-client.js";

/**
 * agentmesh_listen — Autonomous agent mode.
 *
 * TEMPORARILY DISABLED during the session → thread migration.
 *
 * The previous implementation polled session messages via the legacy
 * /api/sessions/:id/messages endpoint. That endpoint has been removed
 * and there is no thread-specific WebSocket/poll channel yet. Channel-
 * scoped subscriptions with client-side thread filtering are planned
 * but require rewiring WS handlers — out of scope for this commit.
 *
 * Re-enable once channel-scoped event subscription is wired.
 */
export function registerListenTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  // client is retained in the signature so callers don't need to change.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _client: HubClient,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _state: { agentId?: string; ownerId?: string; lastProcessedId?: Record<string, string> },
) {
  server.registerTool(
    "agentmesh_listen",
    {
      description:
        "Autonomous listen mode — temporarily disabled during thread migration. " +
        "Once re-enabled, this will subscribe to channel-level events and " +
        "filter by thread client-side.",
      inputSchema: {
        timeoutMs: z.number().int().min(5000).max(300000).optional(),
        channelName: z.string().optional(),
        threadId: z.string().optional(),
      },
    },
    async () => {
      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({
            error:
              "agentmesh_listen is temporarily disabled during the session-to-thread migration. " +
              "The session WebSocket/poll channel has been removed; channel-scoped " +
              "subscription with thread filtering will be added in a follow-up.",
          }),
        }],
        isError: true,
      };
    },
  );
}

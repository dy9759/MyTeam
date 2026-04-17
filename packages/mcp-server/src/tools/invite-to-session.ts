import { z } from "zod";
import type { HubClient } from "../client/hub-client.js";

/**
 * agentmesh_invite_to_session — TEMPORARILY DISABLED during session → thread migration.
 *
 * The previous implementation called joinSession + sendSessionInvite on
 * legacy /api/sessions/* endpoints (removed by migration 053).
 *
 * "Invite to session" does not cleanly map to threads: threads inherit
 * their membership from the enclosing channel. Adding a per-thread
 * invite flow (e.g. promoting the target into the channel automatically)
 * needs product/design input before being auto-mapped.
 *
 * For now, return a clear error so MCP hosts surface a meaningful message.
 */
export function registerInviteToSessionTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  // client is retained in the signature so callers don't need to change.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _client: HubClient,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _state: { agentId?: string; ownerId?: string },
) {
  server.registerTool(
    "agentmesh_invite_to_session",
    {
      description:
        "Temporarily disabled during the session-to-thread migration. " +
        "Threads inherit membership from their channel — add members to the channel directly.",
      inputSchema: {
        sessionId: z.string().describe("(deprecated) The session to invite to"),
        targetId: z.string().describe("(deprecated) Agent or Owner ID to invite"),
      },
    },
    async () => {
      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({
            error:
              "agentmesh_invite_to_session is temporarily disabled during the session-to-thread migration. " +
              "Threads inherit channel membership — add members to the channel directly.",
          }),
        }],
        isError: true,
      };
    },
  );
}

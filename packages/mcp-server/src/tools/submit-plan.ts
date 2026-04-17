import { z } from "zod";
import type { UnifiedClient } from "../client/unified-client.js";

export function registerSubmitPlanTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  client: UnifiedClient,
  state: { agentId?: string; ownerId?: string },
) {
  server.registerTool(
    "agentmesh_submit_plan",
    {
      description:
        "Submit a plan or proposal to a session for review by other participants. " +
        "Sends a plan_request interaction that others can approve or provide feedback on.",
      inputSchema: {
        sessionId: z.string().describe("The session to submit the plan to"),
        targetId: z.string().describe("Agent or Owner ID to send the plan to (usually the coordinator)"),
        plan: z.string().describe("The plan/proposal text"),
        data: z.record(z.unknown()).optional().describe("Optional structured data for the plan"),
      },
    },
    async ({ sessionId, targetId, plan, data }) => {
      if (!state.agentId) {
        return { content: [{ type: "text" as const, text: JSON.stringify({ error: "Not registered." }) }], isError: true };
      }

      try {
        const targetType = targetId.startsWith("owner-") ? "ownerId" : "agentId";
        // plan_request is a Hub-only interaction type; Multica mode has no equivalent yet.
        const hub = client.getHub();
        if (!hub) {
          return {
            content: [{
              type: "text" as const,
              text: JSON.stringify({
                error: "agentmesh_submit_plan is only available in Hub mode. " +
                  "Plan submission has no Multica equivalent yet.",
              }),
            }],
            isError: true,
          };
        }
        const result = await hub.sendInteraction({
          type: "plan_request" as any,
          contentType: "text",
          target: { [targetType]: targetId, sessionId },
          payload: { text: plan, data },
          metadata: { expectReply: true, correlationId: `plan-${Date.now()}` },
        });

        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              sessionId,
              planId: result.id,
              sentTo: targetId,
              message: "Plan submitted for review",
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to submit plan: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );

  // Get thread summary — reads context items filtered by item_type = "summary".
  server.registerTool(
    "agentmesh_session_summary",
    {
      description:
        "Get summary context items attached to a thread. " +
        "Returns all context items with item_type = 'summary' for the given thread.",
      inputSchema: {
        threadId: z.string().describe("The thread to summarize"),
      },
    },
    async ({ threadId }) => {
      try {
        const items = await client.listThreadContextItems(threadId);
        // listThreadContextItems may return either an array or { items: [...] } depending on backend.
        const list: any[] = Array.isArray(items) ? items : (items?.items ?? []);
        const summaries = list.filter((i) => i?.item_type === "summary");
        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              threadId,
              count: summaries.length,
              latest: summaries[0]?.body,
              summaries,
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to get summary: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );
}

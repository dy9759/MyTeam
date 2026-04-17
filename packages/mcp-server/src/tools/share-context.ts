import { z } from "zod";
import type { HubClient } from "../client/hub-client.js";

// Map item_type to its default retention_class per PRD Section 3.2.
// - decision → permanent
// - summary → ttl (caller may override)
// - file / code_snippet / reference → ttl
function defaultRetention(
  itemType: "decision" | "file" | "code_snippet" | "summary" | "reference",
): "permanent" | "ttl" | "temp" {
  return itemType === "decision" ? "permanent" : "ttl";
}

export function registerShareContextTool(
  server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
  client: HubClient,
) {
  server.registerTool(
    "agentmesh_share_context",
    {
      description:
        "Share context (files, code snippets, decisions, summaries) to a thread. " +
        "All thread participants (channel members) can see the shared context. " +
        "Decisions are retained permanently; other item types default to TTL retention.",
      inputSchema: {
        threadId: z.string().describe("The thread to share context with"),
        files: z.array(z.object({
          name: z.string(),
          content: z.string().optional(),
          fileId: z.string().optional(),
        })).optional().describe("Files to share (one context item per file)"),
        codeSnippets: z.array(z.object({
          language: z.string(),
          code: z.string(),
          description: z.string(),
        })).optional().describe("Code snippets to share (one context item per snippet)"),
        decision: z.string().optional().describe("Record a decision made during discussion"),
        summary: z.string().optional().describe("Record a summary of the discussion"),
        references: z.array(z.object({
          title: z.string(),
          url: z.string().optional(),
          body: z.string().optional(),
        })).optional().describe("External references to share"),
      },
    },
    async ({ threadId, files, codeSnippets, decision, summary, references }) => {
      try {
        const created: Array<{ itemType: string; title?: string; id?: string }> = [];

        if (files) {
          for (const f of files) {
            const item = await client.createThreadContextItem(threadId, {
              item_type: "file",
              title: f.name,
              body: f.content,
              metadata: f.fileId ? { fileId: f.fileId } : undefined,
              retention_class: defaultRetention("file"),
            });
            created.push({ itemType: "file", title: f.name, id: item?.id });
          }
        }

        if (codeSnippets) {
          for (const cs of codeSnippets) {
            const item = await client.createThreadContextItem(threadId, {
              item_type: "code_snippet",
              title: cs.description,
              body: cs.code,
              metadata: { language: cs.language },
              retention_class: defaultRetention("code_snippet"),
            });
            created.push({ itemType: "code_snippet", title: cs.description, id: item?.id });
          }
        }

        if (decision) {
          const item = await client.createThreadContextItem(threadId, {
            item_type: "decision",
            body: decision,
            retention_class: defaultRetention("decision"),
          });
          created.push({ itemType: "decision", id: item?.id });
        }

        if (summary) {
          const item = await client.createThreadContextItem(threadId, {
            item_type: "summary",
            body: summary,
            retention_class: defaultRetention("summary"),
          });
          created.push({ itemType: "summary", id: item?.id });
        }

        if (references) {
          for (const r of references) {
            const item = await client.createThreadContextItem(threadId, {
              item_type: "reference",
              title: r.title,
              body: r.body,
              metadata: r.url ? { url: r.url } : undefined,
              retention_class: defaultRetention("reference"),
            });
            created.push({ itemType: "reference", title: r.title, id: item?.id });
          }
        }

        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              threadId,
              sessionId: threadId,
              createdCount: created.length,
              created,
              message: `Shared ${created.length} context item(s) to thread`,
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to share context: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );

  server.registerTool(
    "agentmesh_get_session_context",
    {
      description:
        "Get the shared context of a thread, including files, code snippets, decisions, summaries, and references.",
      inputSchema: {
        threadId: z.string().describe("The thread ID"),
      },
    },
    async ({ threadId }) => {
      try {
        const [thread, items] = await Promise.all([
          client.getThread(threadId),
          client.listThreadContextItems(threadId),
        ]);

        const contextItems: any[] = Array.isArray(items)
          ? items
          : (items?.items ?? items?.context_items ?? []);

        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              threadId,
              sessionId: threadId,
              title: thread?.title,
              status: thread?.status,
              replyCount: thread?.reply_count,
              lastReplyAt: thread?.last_reply_at,
              contextItems,
            }, null, 2),
          }],
        };
      } catch (err: any) {
        return {
          content: [{ type: "text" as const, text: JSON.stringify({ error: `Failed to get thread context: ${err.message ?? err}` }) }],
          isError: true,
        };
      }
    },
  );
}

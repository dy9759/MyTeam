import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { HubClient } from "./client/hub-client.js";
import { MulticaClient } from "./client/multica-client.js";
import { UnifiedClient } from "./client/unified-client.js";
import { registerAgentTool } from "./tools/register.js";
import { registerListAgentsTool } from "./tools/list-agents.js";
import { registerSendMessageTool } from "./tools/send-message.js";
import { registerCheckMessagesTool } from "./tools/check-messages.js";
import { registerBroadcastTool } from "./tools/broadcast.js";
import { registerCreateTaskTool } from "./tools/create-task.js";
import { registerCreateChannelTool } from "./tools/create-channel.js";
import { registerSendToChannelTool } from "./tools/send-to-channel.js";
import { registerListChannelsTool } from "./tools/list-channels.js";
import { registerJoinChannelTool } from "./tools/join-channel.js";
import { registerSendFileTool } from "./tools/send-file.js";
import { registerDownloadFileTool } from "./tools/download-file.js";
import { registerChatTool } from "./tools/chat.js";
import { registerConversationsTool } from "./tools/conversations.js";
import { registerOwnerMessageTools } from "./tools/owner-messages.js";
import { registerCreateSessionTool } from "./tools/create-session.js";
import { registerSessionStatusTool } from "./tools/session-status.js";
import { registerMultiTurnChatTool } from "./tools/multi-turn-chat.js";
import { registerShareContextTool } from "./tools/share-context.js";
import { registerInviteToSessionTool } from "./tools/invite-to-session.js";
import { registerSubmitPlanTool } from "./tools/submit-plan.js";
import { registerTeamTools } from "./tools/team.js";
import { registerRemoteSessionTools } from "./tools/remote-session.js";
import { registerAutoReplyTools } from "./tools/auto-reply-config.js";
import { registerListenTool } from "./tools/listen.js";
import { registerAgentProfileTools } from "./tools/agent-profile.js";
import { sendDesktopNotification } from "./notify.js";

export interface McpServerConfig {
  hubUrl: string;
  apiKey: string;
}

export interface McpServerState {
  agentId?: string;
  ownerId?: string;
  onRegistered?: (agentId: string) => void;
  lastProcessedId?: Record<string, string>;
}

export function createMcpServer(config: McpServerConfig): { server: McpServer; client: HubClient; state: McpServerState } {
  const client = new HubClient({
    hubUrl: config.hubUrl,
    apiKey: config.apiKey,
  });

  // Shared mutable state — agentId is set after registration
  const state: McpServerState = {};

  const server = new McpServer({
    name: "agentmesh",
    version: "0.2.0",
  });

  // Register all tools
  registerAgentTool(server, client, state);
  registerListAgentsTool(server, client);
  registerSendMessageTool(server, client, state);
  registerCheckMessagesTool(server, client, state);
  registerBroadcastTool(server, client);
  registerCreateTaskTool(server, client, state);
  registerCreateChannelTool(server, client, state);
  registerSendToChannelTool(server, client);
  registerListChannelsTool(server, client);
  registerJoinChannelTool(server, client, state);
  registerSendFileTool(server, client, state);
  registerDownloadFileTool(server, client);
  registerChatTool(server, client, state);
  registerConversationsTool(server, client, state);
  registerOwnerMessageTools(server, client, state);
  registerCreateSessionTool(server, client, state);
  registerSessionStatusTool(server, client);
  registerMultiTurnChatTool(server, client, state);
  registerShareContextTool(server, client);
  registerInviteToSessionTool(server, client, state);
  registerSubmitPlanTool(server, client, state);
  registerTeamTools(server, client, state);
  registerRemoteSessionTools(server, client, state);
  registerAutoReplyTools(server, client, state);
  registerListenTool(server, client, state);
  registerAgentProfileTools(server, client, state);

  return { server, client, state };
}

// CLI entrypoint: stdio transport
async function main() {
  const hubUrl = process.env.AGENTMESH_HUB_URL;
  const apiKey = process.env.AGENTMESH_API_KEY ?? "";

  // Multica Go backend support
  const multicaUrl = process.env.MULTICA_API_URL;
  const multicaToken = process.env.MULTICA_TOKEN;
  const multicaWorkspace = process.env.MULTICA_WORKSPACE_ID;

  let multicaClient: MulticaClient | undefined;
  if (multicaUrl) {
    console.error(`[agentmesh-mcp] Multica mode: ${multicaUrl}`);
    multicaClient = new MulticaClient({
      baseUrl: multicaUrl,
      token: multicaToken ?? "",
      workspaceId: multicaWorkspace ?? "",
    });
  }

  if (!hubUrl && !multicaUrl) {
    console.error("[agentmesh-mcp] Error: AGENTMESH_HUB_URL or MULTICA_API_URL is required");
    process.exit(1);
  }

  if (!apiKey) {
    console.error("[agentmesh-mcp] Warning: AGENTMESH_API_KEY not set");
  }

  const { server, client, state } = createMcpServer({ hubUrl: hubUrl ?? "", apiKey });
  const transport = new StdioServerTransport();

  // Create UnifiedClient — tools can use this for either backend
  const unifiedClient = new UnifiedClient(
    hubUrl ? client : undefined,
    multicaClient,
  );
  console.error(`[agentmesh-mcp] Mode: ${unifiedClient.isMulticaMode ? "Multica Go" : "Hub Node.js"}`);

  // Resolve ownerId from API key via whoami endpoint
  if (hubUrl) {
    try {
      const owner = await client.whoami();
      state.ownerId = owner.ownerId;
      console.error(`[agentmesh-mcp] Hub reachable at ${hubUrl}, owner: ${owner.ownerId} (${owner.name})`);
    } catch (err) {
      console.error(`[agentmesh-mcp] Warning: Hub not reachable at ${hubUrl}:`, err);
    }
  }

  // After agent registration, connect WebSocket for real-time notifications (Hub mode only)
  state.onRegistered = (agentId: string) => {
    if (!hubUrl) return;
    client.connectWebSocket(agentId);

    // Auto-refresh JWT every 50 minutes (before 1h expiry)
    const refreshInterval = setInterval(async () => {
      try {
        const result = await client.refreshAgentToken(agentId);
        client.setAgentToken(result.agentToken);
        console.error(`[agentmesh-mcp] Token refreshed for ${agentId}`);
      } catch (err) {
        console.error(`[agentmesh-mcp] Token refresh failed:`, err);
      }
    }, 50 * 60 * 1000); // 50 minutes

    client.onInteraction((interaction) => {
      const fromId = interaction.fromId ?? interaction.fromAgent;
      const preview = interaction.payload.text?.slice(0, 100) || (interaction.payload.file ? `[File: ${interaction.payload.file.fileName}]` : "[message]");

      sendDesktopNotification(
        "AgentMesh",
        `${fromId}: ${preview}`,
      );

      server.sendLoggingMessage({
        level: "info",
        data: `[AgentMesh] New message from ${fromId}: ${preview}`,
      }).catch(() => {});
    });
    console.error(`[agentmesh-mcp] WebSocket connected for agent ${agentId}`);
  };

  await server.connect(transport);
  console.error(`[agentmesh-mcp] Connected to Hub at ${hubUrl}`);
}

main().catch((err) => {
  console.error("[agentmesh-mcp] Fatal error:", err);
  process.exit(1);
});

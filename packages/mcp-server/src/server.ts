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
  multicaClient?: MulticaClient;
}

export interface McpServerState {
  agentId?: string;
  ownerId?: string;
  onRegistered?: (agentId: string) => void;
  lastProcessedId?: Record<string, string>;
}

export function createMcpServer(
  config: McpServerConfig,
): { server: McpServer; client: HubClient; unifiedClient: UnifiedClient; state: McpServerState } {
  const hubClient = config.hubUrl
    ? new HubClient({ hubUrl: config.hubUrl, apiKey: config.apiKey })
    : new HubClient({ hubUrl: "", apiKey: config.apiKey });

  // UnifiedClient routes thread calls to Multica when Multica is configured,
  // and falls back to Hub otherwise. Thread-using tools must use unifiedClient
  // so Multica-only mode hits the Go backend instead of the dead session API.
  const unifiedClient = new UnifiedClient(
    config.hubUrl ? hubClient : undefined,
    config.multicaClient,
  );

  // Shared mutable state — agentId is set after registration
  const state: McpServerState = {};

  const server = new McpServer({
    name: "agentmesh",
    version: "0.2.0",
  });

  // Register all tools
  registerAgentTool(server, hubClient, state);
  registerListAgentsTool(server, hubClient);
  registerSendMessageTool(server, hubClient, state);
  registerCheckMessagesTool(server, hubClient, state);
  registerBroadcastTool(server, hubClient);
  registerCreateTaskTool(server, hubClient, state);
  registerCreateChannelTool(server, hubClient, state);
  registerSendToChannelTool(server, hubClient);
  registerListChannelsTool(server, hubClient);
  registerJoinChannelTool(server, hubClient, state);
  registerSendFileTool(server, hubClient, state);
  registerDownloadFileTool(server, hubClient);
  registerChatTool(server, hubClient, state);
  registerConversationsTool(server, hubClient, state);
  registerOwnerMessageTools(server, hubClient, state);
  // Thread-based tools route through UnifiedClient.
  registerCreateSessionTool(server, unifiedClient, state);
  registerSessionStatusTool(server, unifiedClient);
  registerMultiTurnChatTool(server, unifiedClient, state);
  registerShareContextTool(server, unifiedClient);
  registerInviteToSessionTool(server, hubClient, state);
  registerSubmitPlanTool(server, unifiedClient, state);
  registerTeamTools(server, hubClient, state);
  registerRemoteSessionTools(server, hubClient, state);
  registerAutoReplyTools(server, hubClient, state);
  registerListenTool(server, hubClient, state);
  registerAgentProfileTools(server, hubClient, state);

  return { server, client: hubClient, unifiedClient, state };
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

  const { server, client, unifiedClient, state } = createMcpServer({
    hubUrl: hubUrl ?? "",
    apiKey,
    multicaClient,
  });
  const transport = new StdioServerTransport();

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

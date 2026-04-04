import { HubClient } from "./hub-client.js";
import { MulticaClient } from "./multica-client.js";

/**
 * UnifiedClient wraps either HubClient or MulticaClient,
 * providing a common interface for all MCP tools.
 */
export class UnifiedClient {
  private hub?: HubClient;
  private multica?: MulticaClient;

  constructor(hub?: HubClient, multica?: MulticaClient) {
    this.hub = hub;
    this.multica = multica;
  }

  get isMulticaMode(): boolean { return !!this.multica; }
  get isHubMode(): boolean { return !!this.hub && !this.multica; }

  // Use multica if available, fallback to hub
  private get client(): HubClient | MulticaClient {
    return (this.multica ?? this.hub)!;
  }

  // ─── Agents ─────────────
  async listAgents(): Promise<{ agents: any[] }> {
    if (this.multica) return this.multica.listAgents();
    return this.hub!.listAgents();
  }

  async getAgent(id: string): Promise<any> {
    if (this.multica) return this.multica.getAgent(id);
    return this.hub!.getAgent(id);
  }

  // ─── Messages ─────────────
  async sendMessage(target: { channelId?: string; recipientId?: string; recipientType?: string; sessionId?: string }, content: string): Promise<any> {
    if (this.multica) {
      return this.multica.sendMessage({
        channel_id: target.channelId,
        recipient_id: target.recipientId,
        recipient_type: target.recipientType,
        session_id: target.sessionId,
        content,
      });
    }
    // Hub format
    return this.hub!.sendInteraction({
      type: "message",
      contentType: "text",
      target: {
        agentId: target.recipientId,
        channel: target.channelId,
        sessionId: target.sessionId,
      },
      payload: { text: content },
    });
  }

  async listMessages(params: Record<string, string>): Promise<{ messages: any[] }> {
    if (this.multica) return this.multica.listMessages(params);
    // Hub: use pollInteractions
    const agentId = params.agent_id ?? params.recipient_id;
    if (agentId) {
      const result = await this.hub!.pollInteractions(agentId);
      return { messages: result.interactions ?? [] };
    }
    return { messages: [] };
  }

  async listConversations(): Promise<{ conversations: any[] }> {
    if (this.multica) return this.multica.listConversations();
    return { conversations: [] };
  }

  // ─── Channels ─────────────
  async listChannels(): Promise<{ channels: any[] }> {
    if (this.multica) return this.multica.listChannels();
    return this.hub!.listChannels();
  }

  async createChannel(data: { name: string; description?: string }): Promise<any> {
    if (this.multica) return this.multica.createChannel(data);
    return this.hub!.createChannel(data);
  }

  async joinChannel(id: string): Promise<void> {
    if (this.multica) return this.multica.joinChannel(id);
    return this.hub!.joinChannel(id);
  }

  async getChannelMessages(id: string): Promise<{ messages: any[] }> {
    if (this.multica) return this.multica.getChannelMessages(id);
    const result = await this.hub!.getChannelMessages(id);
    return { messages: (result as any).interactions ?? [] };
  }

  async getChannelMembers(id: string): Promise<{ members: any[] }> {
    if (this.multica) return this.multica.getChannelMembers(id);
    return this.hub!.getChannelMembers(id);
  }

  // ─── Sessions ─────────────
  async createSession(data: any): Promise<any> {
    if (this.multica) return this.multica.createSession(data);
    return this.hub!.createSession(data);
  }

  async listSessions(): Promise<{ sessions: any[] }> {
    if (this.multica) return this.multica.listSessions();
    return this.hub!.listSessions();
  }

  async getSession(id: string): Promise<any> {
    if (this.multica) return this.multica.getSession(id);
    return this.hub!.getSession(id);
  }

  async getSessionMessages(id: string): Promise<{ messages: any[] }> {
    if (this.multica) return this.multica.getSessionMessages(id);
    return this.hub!.getSessionMessages(id);
  }

  async updateSession(id: string, data: any): Promise<any> {
    if (this.multica) return this.multica.updateSession(id, data);
    return this.hub!.updateSession(id, data);
  }

  async joinSession(id: string): Promise<void> {
    if (this.multica) return this.multica.joinSession(id);
    return this.hub!.joinSession(id);
  }

  async getSessionSummary(id: string): Promise<any> {
    if (this.multica) return this.multica.getSessionSummary(id);
    return this.hub!.getSessionSummary(id);
  }

  // ─── Issues (Multica only) ─────────────
  async listIssues(params?: Record<string, string>): Promise<{ issues: any[] }> {
    if (this.multica) return this.multica.listIssues(params);
    return { issues: [] };
  }

  async getIssue(id: string): Promise<any> {
    if (this.multica) return this.multica.getIssue(id);
    throw new Error("Issues not available in Hub mode");
  }

  async createIssue(data: any): Promise<any> {
    if (this.multica) return this.multica.createIssue(data);
    throw new Error("Issues not available in Hub mode");
  }

  // ─── Listen (long-poll) ─────────────
  async listen(params: Record<string, string>): Promise<any> {
    if (this.multica) {
      return this.multica.listen(params);
    }
    // Hub mode: use existing poll
    return { messages: [], has_new: false, poll_status: "timeout" };
  }

  // ─── Health ─────────────
  async health(): Promise<any> {
    if (this.multica) return this.multica.health();
    return this.hub!.health();
  }

  // ─── Passthrough for Hub-specific methods ─────────────
  getHub(): HubClient | undefined { return this.hub; }
  getMultica(): MulticaClient | undefined { return this.multica; }
}

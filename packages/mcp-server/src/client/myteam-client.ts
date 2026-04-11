/**
 * MyTeamClient — Connects MCP tools to MyTeam's Go backend.
 *
 * Differences from HubClient:
 * - Auth: JWT token + X-Workspace-ID header (not just Bearer API key)
 * - API paths: /api/messages, /api/channels, /api/sessions (not /api/interactions)
 * - Workspace-scoped: all requests need workspace_id
 */

export interface MyTeamClientConfig {
  baseUrl: string; // e.g. "http://localhost:8080"
  token: string; // JWT token
  workspaceId: string; // Active workspace
}

export class MyTeamClient {
  private baseUrl: string;
  private token: string;
  private workspaceId: string;

  constructor(config: MyTeamClientConfig) {
    this.baseUrl = config.baseUrl.replace(/\/$/, "");
    this.token = config.token;
    this.workspaceId = config.workspaceId;
  }

  setToken(token: string): void {
    this.token = token;
  }
  setWorkspaceId(id: string): void {
    this.workspaceId = id;
  }

  private async fetch<T>(path: string, opts: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const headers: Record<string, string> = {
      "X-Workspace-ID": this.workspaceId,
      ...(opts.headers as Record<string, string>),
    };
    if (opts.body) {
      headers["Content-Type"] = "application/json";
    }
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch(url, { ...opts, headers });
    if (!response.ok) {
      const body = await response.text();
      throw new Error(`MyTeam API ${response.status}: ${body}`);
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }

  // ─── Messages ──────────────────────────────────

  async sendMessage(body: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: string;
    session_id?: string;
    content: string;
    content_type?: string;
    file_id?: string;
    file_name?: string;
    metadata?: Record<string, unknown>;
  }): Promise<any> {
    return this.fetch("/api/messages", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async listMessages(
    params: Record<string, string>,
  ): Promise<{ messages: any[] }> {
    const qs = new URLSearchParams(params).toString();
    return this.fetch(`/api/messages?${qs}`);
  }

  async listConversations(): Promise<{ conversations: any[] }> {
    return this.fetch("/api/messages/conversations");
  }

  // ─── Channels ──────────────────────────────────

  async createChannel(body: {
    name: string;
    description?: string;
  }): Promise<any> {
    return this.fetch("/api/channels", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async listChannels(): Promise<{ channels: any[] }> {
    return this.fetch("/api/channels");
  }

  async getChannel(id: string): Promise<any> {
    return this.fetch(`/api/channels/${id}`);
  }

  async joinChannel(id: string): Promise<void> {
    return this.fetch(`/api/channels/${id}/join`, { method: "POST" });
  }

  async leaveChannel(id: string): Promise<void> {
    return this.fetch(`/api/channels/${id}/leave`, { method: "POST" });
  }

  async getChannelMembers(id: string): Promise<{ members: any[] }> {
    return this.fetch(`/api/channels/${id}/members`);
  }

  async getChannelMessages(
    id: string,
    params?: Record<string, string>,
  ): Promise<{ messages: any[] }> {
    const qs = params ? `?${new URLSearchParams(params)}` : "";
    return this.fetch(`/api/channels/${id}/messages${qs}`);
  }

  // ─── Sessions ──────────────────────────────────

  async createSession(body: {
    title: string;
    issue_id?: string;
    max_turns?: number;
    context?: Record<string, unknown>;
    participants?: Array<{ id: string; type: string }>;
  }): Promise<any> {
    return this.fetch("/api/sessions", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async listSessions(
    params?: Record<string, string>,
  ): Promise<{ sessions: any[] }> {
    const qs = params ? `?${new URLSearchParams(params)}` : "";
    return this.fetch(`/api/sessions${qs}`);
  }

  async getSession(id: string): Promise<any> {
    return this.fetch(`/api/sessions/${id}`);
  }

  async updateSession(
    id: string,
    body: Record<string, unknown>,
  ): Promise<any> {
    return this.fetch(`/api/sessions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  async joinSession(id: string): Promise<void> {
    return this.fetch(`/api/sessions/${id}/join`, { method: "POST" });
  }

  async getSessionMessages(id: string): Promise<{ messages: any[] }> {
    return this.fetch(`/api/sessions/${id}/messages`);
  }

  async getSessionSummary(id: string): Promise<any> {
    return this.fetch(`/api/sessions/${id}/summary`);
  }

  // ─── Issues (MyTeam native) ───────────────────

  async listIssues(
    params?: Record<string, string>,
  ): Promise<{ issues: any[] }> {
    const qs = params ? `?${new URLSearchParams(params)}` : "";
    return this.fetch(`/api/issues${qs}`);
  }

  async getIssue(id: string): Promise<any> {
    return this.fetch(`/api/issues/${id}`);
  }

  async createIssue(body: {
    title: string;
    description?: string;
    status?: string;
    priority?: string;
    assignee_id?: string;
    assignee_type?: string;
  }): Promise<any> {
    return this.fetch("/api/issues", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateIssue(
    id: string,
    body: Record<string, unknown>,
  ): Promise<any> {
    return this.fetch(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  // ─── Agents ────────────────────────────────────

  async listAgents(): Promise<{ agents: any[] }> {
    return this.fetch("/api/agents");
  }

  async getAgent(id: string): Promise<any> {
    return this.fetch(`/api/agents/${id}`);
  }

  // ─── Auth ──────────────────────────────────────

  async getCurrentUser(): Promise<any> {
    return this.fetch("/api/me");
  }

  // ─── Listen (long-poll) ────────────────────────

  async listen(params: Record<string, string>): Promise<any> {
    const qs = new URLSearchParams(params).toString();
    return this.fetch(`/api/listen?${qs}`);
  }

  // ─── Plans (MyTeam native) ───────────────────

  async generatePlan(input: string): Promise<any> {
    return this.fetch("/api/plans/generate", { method: "POST", body: JSON.stringify({ input }) });
  }

  // ─── Remote sessions ─────────────────────────

  async createRemoteSession(agentId: string, title?: string): Promise<any> {
    return this.fetch("/api/remote-sessions", { method: "POST", body: JSON.stringify({ agent_id: agentId, title }) });
  }

  async getRemoteSession(id: string): Promise<any> {
    return this.fetch(`/api/remote-sessions/${id}`);
  }

  async listRemoteSessions(): Promise<any> {
    return this.fetch("/api/remote-sessions");
  }

  // ─── Typing ───────────────────────────────────

  async sendTyping(channelId?: string, sessionId?: string): Promise<void> {
    return this.fetch("/api/typing", { method: "POST", body: JSON.stringify({ channel_id: channelId, session_id: sessionId, is_typing: true }) });
  }

  // ─── Skill broadcast ─────────────────────────

  async skillBroadcast(skillId: string, text: string): Promise<any> {
    return this.fetch(`/api/skills/${skillId}/broadcast`, { method: "POST", body: JSON.stringify({ text }) });
  }

  // ─── Health ────────────────────────────────────

  async health(): Promise<any> {
    return this.fetch("/health");
  }
}

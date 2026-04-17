import type {
  Agent,
  AgentRegistration,
  AgentHeartbeat,
  AgentFilter,
  Interaction,
  SendInteractionRequest,
  Channel,
  CreateChannelRequest,
  Task,
  CreateTaskRequest,
  UpdateTaskStatusRequest,
  RegisterResponse,
  ListAgentsResponse,
  ListInteractionsResponse,
  ListChannelsResponse,
  CreateTaskResponse,
  ListTasksResponse,
  HealthResponse,
  CreateOwnerResponse,
  WSMessage,
  WSInteractionPayload,
} from "@agentmesh/shared";
import WebSocket from "ws";

export interface HubClientConfig {
  hubUrl: string;
  apiKey?: string;
  agentToken?: string;
}

export class HubClient {
  private hubUrl: string;
  private token: string;
  private ws: WebSocket | null = null;
  private wsReconnectTimer: NodeJS.Timeout | null = null;
  private wsReconnectDelay = 5000;
  private interactionCallbacks: Array<(interaction: Interaction) => void> = [];
  private wsConnected = false;

  constructor(config: HubClientConfig) {
    this.hubUrl = config.hubUrl.replace(/\/$/, "");
    this.token = config.agentToken || config.apiKey || "";
  }

  setAgentToken(token: string): void {
    this.token = token;
  }

  getAgentToken(): string {
    return this.token;
  }

  // WebSocket methods
  connectWebSocket(agentId: string): void {
    if (this.ws) return;

    const wsUrl = this.hubUrl.replace(/^http/, "ws") + "/ws";

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.on("open", () => {
        this.ws!.send(JSON.stringify({
          type: "hello",
          payload: { agentId, agentToken: this.token },
        }));
      });

      this.ws.on("message", (data) => {
        try {
          const msg: WSMessage = JSON.parse(data.toString());
          this.handleWSMessage(msg);
        } catch {
          // ignore parse errors
        }
      });

      this.ws.on("close", () => {
        this.wsConnected = false;
        this.ws = null;
        this.scheduleReconnect(agentId);
      });

      this.ws.on("error", () => {
        // error event is followed by close event
      });
    } catch {
      this.scheduleReconnect(agentId);
    }
  }

  private handleWSMessage(msg: WSMessage): void {
    switch (msg.type) {
      case "ack":
        this.wsConnected = true;
        this.wsReconnectDelay = 5000; // reset backoff
        console.log("[hub-client] WebSocket authenticated");
        break;
      case "interaction": {
        const payload = msg.payload as WSInteractionPayload;
        if (payload?.interaction) {
          for (const cb of this.interactionCallbacks) {
            try { cb(payload.interaction); } catch { /* ignore */ }
          }
        }
        break;
      }
      case "ping":
        this.ws?.send(JSON.stringify({ type: "pong" }));
        break;
      case "error":
        console.error("[hub-client] WS error:", msg.payload);
        break;
    }
  }

  private scheduleReconnect(agentId: string): void {
    if (this.wsReconnectTimer) return;
    this.wsReconnectTimer = setTimeout(() => {
      this.wsReconnectTimer = null;
      this.connectWebSocket(agentId);
      // Exponential backoff: 5s → 10s → 20s → 30s max
      this.wsReconnectDelay = Math.min(this.wsReconnectDelay * 2, 30000);
    }, this.wsReconnectDelay);
  }

  onInteraction(callback: (interaction: Interaction) => void): void {
    this.interactionCallbacks.push(callback);
  }

  isWebSocketConnected(): boolean {
    return this.wsConnected;
  }

  disconnectWebSocket(): void {
    if (this.wsReconnectTimer) {
      clearTimeout(this.wsReconnectTimer);
      this.wsReconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.wsConnected = false;
  }

  private async fetch<T>(
    path: string,
    opts: RequestInit = {},
  ): Promise<T> {
    const url = `${this.hubUrl}${path}`;
    const headers: Record<string, string> = {
      ...(opts.headers as Record<string, string>),
    };
    // Only set Content-Type if there's a body
    if (opts.body) {
      headers["Content-Type"] = "application/json";
    }
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch(url, {
      ...opts,
      headers,
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`Hub API error ${response.status}: ${body}`);
    }

    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }

  // Owner
  async createOwner(name: string): Promise<CreateOwnerResponse> {
    return this.fetch("/api/owners", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  }

  // Agent registration
  async register(registration: AgentRegistration): Promise<RegisterResponse> {
    return this.fetch("/api/register", {
      method: "POST",
      body: JSON.stringify(registration),
    });
  }

  async heartbeat(hb: AgentHeartbeat): Promise<Agent> {
    return this.fetch("/api/heartbeat", {
      method: "POST",
      body: JSON.stringify(hb),
    });
  }

  async unregister(agentId: string): Promise<void> {
    return this.fetch(`/api/agents/${agentId}`, {
      method: "DELETE",
    });
  }

  // Agent discovery
  async listAgents(filter?: AgentFilter): Promise<ListAgentsResponse> {
    const params = new URLSearchParams();
    if (filter?.type) params.set("type", filter.type);
    if (filter?.capability) params.set("capability", filter.capability);
    if (filter?.status) params.set("status", filter.status);
    if (filter?.maxLoad !== undefined)
      params.set("maxLoad", String(filter.maxLoad));

    const qs = params.toString();
    return this.fetch(`/api/agents${qs ? `?${qs}` : ""}`);
  }

  async getAgent(agentId: string): Promise<Agent> {
    return this.fetch(`/api/agents/${agentId}`);
  }

  async matchAgents(
    capability: string,
    maxLoad?: number,
  ): Promise<ListAgentsResponse> {
    const params = new URLSearchParams({ capability });
    if (maxLoad !== undefined) params.set("maxLoad", String(maxLoad));
    return this.fetch(`/api/agents/match?${params}`);
  }

  // Interactions
  async sendInteraction(
    request: SendInteractionRequest,
  ): Promise<{ id: string; delivered: boolean }> {
    return this.fetch("/api/interactions", {
      method: "POST",
      body: JSON.stringify(request),
    });
  }

  async pollInteractions(
    agentId: string,
    opts?: { afterId?: string; limit?: number },
  ): Promise<ListInteractionsResponse> {
    const params = new URLSearchParams({ agentId });
    if (opts?.afterId) params.set("afterId", opts.afterId);
    if (opts?.limit) params.set("limit", String(opts.limit));
    return this.fetch(`/api/interactions?${params}`);
  }

  // Conversations
  async getConversations(
    agentId: string,
  ): Promise<{ conversations: Array<{ agentId: string; lastMessage: Interaction; lastMessageAt: string }> }> {
    return this.fetch(`/api/conversations?agentId=${encodeURIComponent(agentId)}`);
  }

  async getOwnerConversations(
    ownerId: string,
  ): Promise<{ conversations: Array<{ peerId: string; peerType: string; lastMessage: Interaction; lastMessageAt: string }> }> {
    return this.fetch(`/api/conversations?ownerId=${encodeURIComponent(ownerId)}`);
  }

  async getChatHistory(
    myId: string,
    otherId: string,
    opts?: { afterId?: string; limit?: number },
    idType: "agentId" | "ownerId" = "agentId",
  ): Promise<{ messages: Interaction[] }> {
    const params = new URLSearchParams({ [idType]: myId });
    if (opts?.afterId) params.set("afterId", opts.afterId);
    if (opts?.limit) params.set("limit", String(opts.limit));
    return this.fetch(`/api/conversations/${encodeURIComponent(otherId)}/messages?${params}`);
  }

  // Owner inbox
  async pollOwnerInteractions(
    ownerId: string,
    opts?: { afterId?: string; limit?: number },
  ): Promise<ListInteractionsResponse> {
    const params = new URLSearchParams({ ownerId });
    if (opts?.afterId) params.set("afterId", opts.afterId);
    if (opts?.limit) params.set("limit", String(opts.limit));
    return this.fetch(`/api/interactions?${params}`);
  }

  // Channels
  async createChannel(request: CreateChannelRequest): Promise<Channel> {
    return this.fetch("/api/channels", {
      method: "POST",
      body: JSON.stringify(request),
    });
  }

  async listChannels(): Promise<ListChannelsResponse> {
    return this.fetch("/api/channels");
  }

  async joinChannel(channelName: string): Promise<void> {
    return this.fetch(`/api/channels/${channelName}/join`, { method: "POST" });
  }

  async getChannelMessages(
    channelName: string,
    opts?: { afterId?: string; limit?: number },
  ): Promise<ListInteractionsResponse> {
    const params = new URLSearchParams();
    if (opts?.afterId) params.set("afterId", opts.afterId);
    if (opts?.limit) params.set("limit", String(opts.limit));
    const qs = params.toString();
    return this.fetch(
      `/api/channels/${channelName}/messages${qs ? `?${qs}` : ""}`,
    );
  }

  // Tasks
  async createTask(request: CreateTaskRequest): Promise<CreateTaskResponse> {
    return this.fetch("/api/tasks", {
      method: "POST",
      body: JSON.stringify(request),
    });
  }

  async listTasks(opts?: {
    status?: string;
    assignedTo?: string;
    createdBy?: string;
  }): Promise<ListTasksResponse> {
    const params = new URLSearchParams();
    if (opts?.status) params.set("status", opts.status);
    if (opts?.assignedTo) params.set("assignedTo", opts.assignedTo);
    if (opts?.createdBy) params.set("createdBy", opts.createdBy);
    const qs = params.toString();
    return this.fetch(`/api/tasks${qs ? `?${qs}` : ""}`);
  }

  async getTask(taskId: string): Promise<Task> {
    return this.fetch(`/api/tasks/${taskId}`);
  }

  async assignTask(taskId: string, agentId: string): Promise<Task> {
    return this.fetch(`/api/tasks/${taskId}/assign`, {
      method: "POST",
      body: JSON.stringify({ agentId }),
    });
  }

  async updateTaskStatus(
    taskId: string,
    request: UpdateTaskStatusRequest,
  ): Promise<Task> {
    return this.fetch(`/api/tasks/${taskId}/status`, {
      method: "POST",
      body: JSON.stringify(request),
    });
  }

  // Sessions
  async createSession(request: { title: string; participants: Array<{ id: string; type: string }>; maxTurns?: number; context?: Record<string, unknown> }): Promise<any> {
    return this.fetch("/api/sessions", { method: "POST", body: JSON.stringify(request) });
  }

  async getSession(sessionId: string): Promise<any> {
    return this.fetch(`/api/sessions/${sessionId}`);
  }

  async updateSession(sessionId: string, updates: Record<string, unknown>): Promise<any> {
    return this.fetch(`/api/sessions/${sessionId}`, { method: "PATCH", body: JSON.stringify(updates) });
  }

  async getSessionMessages(sessionId: string, opts?: { afterId?: string; limit?: number }): Promise<any> {
    const params = new URLSearchParams();
    if (opts?.afterId) params.set("afterId", opts.afterId);
    if (opts?.limit) params.set("limit", String(opts.limit));
    const qs = params.toString();
    return this.fetch(`/api/sessions/${sessionId}/messages${qs ? `?${qs}` : ""}`);
  }

  async joinSession(sessionId: string): Promise<void> {
    return this.fetch(`/api/sessions/${sessionId}/join`, { method: "POST" });
  }

  async listSessions(opts?: { status?: string; creatorId?: string }): Promise<any> {
    const params = new URLSearchParams();
    if (opts?.status) params.set("status", opts.status);
    if (opts?.creatorId) params.set("creatorId", opts.creatorId);
    const qs = params.toString();
    return this.fetch(`/api/sessions${qs ? `?${qs}` : ""}`);
  }

  // Session summary
  async getSessionSummary(sessionId: string): Promise<any> {
    return this.fetch(`/api/sessions/${sessionId}/summary`);
  }

  async sendSessionInvite(sessionId: string, targetId: string, targetType: "agent" | "owner"): Promise<any> {
    return this.sendInteraction({
      type: "session_invite" as any,
      contentType: "text",
      target: targetType === "agent" ? { agentId: targetId } : { ownerId: targetId },
      payload: { data: { sessionId } },
      metadata: { expectReply: true },
    });
  }

  // Threads
  async createThread(request: { channel_id: string; title?: string; root_message_id?: string; issue_id?: string }): Promise<any> {
    return this.fetch(`/api/channels/${request.channel_id}/threads`, {
      method: "POST",
      body: JSON.stringify({
        title: request.title,
        root_message_id: request.root_message_id,
        issue_id: request.issue_id,
      }),
    });
  }

  async getThread(threadId: string): Promise<any> {
    return this.fetch(`/api/threads/${threadId}`);
  }

  async listThreadMessages(threadId: string, opts?: { limit?: number; offset?: number }): Promise<any> {
    const params = new URLSearchParams();
    if (opts?.limit != null) params.set("limit", String(opts.limit));
    if (opts?.offset != null) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return this.fetch(`/api/threads/${threadId}/messages${qs ? `?${qs}` : ""}`);
  }

  async postThreadMessage(threadId: string, body: { content: string }): Promise<any> {
    return this.fetch(`/api/threads/${threadId}/messages`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async listThreadContextItems(threadId: string): Promise<any> {
    return this.fetch(`/api/threads/${threadId}/context-items`);
  }

  async createThreadContextItem(threadId: string, body: {
    item_type: "decision" | "file" | "code_snippet" | "summary" | "reference";
    title?: string;
    body?: string;
    metadata?: Record<string, unknown>;
    retention_class?: "permanent" | "ttl" | "temp";
  }): Promise<any> {
    return this.fetch(`/api/threads/${threadId}/context-items`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async deleteThreadContextItem(threadId: string, itemId: string): Promise<void> {
    await this.fetch<void>(`/api/threads/${threadId}/context-items/${itemId}`, { method: "DELETE" });
  }

  // Health
  // Owner whoami
  async whoami(): Promise<{ ownerId: string; name: string }> {
    return this.fetch("/api/owners/me");
  }

  async health(): Promise<HealthResponse> {
    return this.fetch("/health");
  }

  // Files
  async uploadFile(
    filePath: string,
  ): Promise<{ id: string; fileName: string; size: number; expiresAt: string }> {
    const { readFileSync } = await import("node:fs");
    const { basename } = await import("node:path");

    const fileName = basename(filePath);
    const fileBuffer = readFileSync(filePath);

    // Build multipart body manually (Node.js built-in FormData not available in all envs)
    const boundary = `----AgentMesh${Date.now()}`;
    const header = `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${fileName}"\r\nContent-Type: application/octet-stream\r\n\r\n`;
    const footer = `\r\n--${boundary}--\r\n`;

    const body = Buffer.concat([
      Buffer.from(header),
      fileBuffer,
      Buffer.from(footer),
    ]);

    const url = `${this.hubUrl}/api/files`;
    const headers: Record<string, string> = {
      "Content-Type": `multipart/form-data; boundary=${boundary}`,
    };
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body,
    });

    if (!response.ok) {
      const text = await response.text();
      throw new Error(`File upload failed ${response.status}: ${text}`);
    }

    return response.json() as Promise<{
      id: string;
      fileName: string;
      size: number;
      expiresAt: string;
    }>;
  }

  // Teams
  async createTeam(request: { name: string; description?: string; members?: Array<{ id: string; type: string }> }): Promise<any> {
    return this.fetch("/api/teams", { method: "POST", body: JSON.stringify(request) });
  }
  async getTeam(teamId: string): Promise<any> {
    return this.fetch(`/api/teams/${teamId}`);
  }
  async listTeams(): Promise<any> {
    return this.fetch("/api/teams");
  }
  async addTeamMember(teamId: string, memberId: string, memberType: string): Promise<any> {
    return this.fetch(`/api/teams/${teamId}/members`, { method: "POST", body: JSON.stringify({ memberId, memberType }) });
  }
  async teamBroadcast(teamId: string, text: string): Promise<any> {
    return this.fetch(`/api/teams/${teamId}/broadcast`, { method: "POST", body: JSON.stringify({ text }) });
  }

  // Remote Sessions
  async createRemoteSession(agentId: string, title?: string): Promise<any> {
    return this.fetch("/api/remote-sessions", { method: "POST", body: JSON.stringify({ agentId, title }) });
  }
  async getRemoteSession(id: string): Promise<any> {
    return this.fetch(`/api/remote-sessions/${id}`);
  }
  async updateRemoteSessionStatus(id: string, status: string): Promise<any> {
    return this.fetch(`/api/remote-sessions/${id}/status`, { method: "PATCH", body: JSON.stringify({ status }) });
  }
  async listRemoteSessions(): Promise<any> {
    return this.fetch("/api/remote-sessions");
  }

  // Auto-reply config
  async getAutoReplyConfig(agentId: string): Promise<any> {
    return this.fetch(`/api/agents/${agentId}/auto-reply`);
  }

  async updateAutoReplyConfig(agentId: string, config: Record<string, unknown>): Promise<any> {
    return this.fetch(`/api/agents/${agentId}/auto-reply`, { method: "PATCH", body: JSON.stringify(config) });
  }

  // Session auto-discussion
  async startAutoDiscussion(sessionId: string): Promise<any> {
    return this.fetch(`/api/sessions/${sessionId}/auto-start`, { method: "POST" });
  }

  async stopAutoDiscussion(sessionId: string): Promise<any> {
    return this.fetch(`/api/sessions/${sessionId}/auto-stop`, { method: "POST" });
  }

  async getChannelMembers(channelName: string): Promise<{ members: Array<{ agentId: string; name: string; type: string; capabilities: string[]; status: string; joinedAt: string }> }> {
    return this.fetch(`/api/channels/${channelName}/members`);
  }

  // Agent Profile
  async getAgentProfile(agentId: string): Promise<any> {
    return this.fetch(`/api/agents/${agentId}/profile`);
  }

  async updateAgentProfile(agentId: string, profile: Record<string, unknown>): Promise<any> {
    return this.fetch(`/api/agents/${agentId}/profile`, {
      method: "PATCH",
      body: JSON.stringify(profile),
    });
  }

  async rotateApiKey(): Promise<{ ownerId: string; apiKey: string }> {
    return this.fetch("/api/owners/rotate-key", { method: "POST" });
  }

  async refreshAgentToken(agentId: string): Promise<{ agentId: string; agentToken: string; expiresIn: number }> {
    return this.fetch(`/api/agents/${agentId}/refresh-token`, { method: "POST" });
  }

  async downloadFile(
    fileId: string,
    destPath: string,
  ): Promise<{ filePath: string; fileName: string; size: number }> {
    const { writeFileSync } = await import("node:fs");

    const url = `${this.hubUrl}/api/files/${fileId}`;
    const headers: Record<string, string> = {};
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }

    const response = await fetch(url, { headers });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(`File download failed ${response.status}: ${text}`);
    }

    const disposition = response.headers.get("content-disposition") || "";
    const match = disposition.match(/filename="([^"]+)"/);
    const fileName = match ? match[1] : `file-${fileId}`;

    const arrayBuffer = await response.arrayBuffer();
    const buffer = Buffer.from(arrayBuffer);
    writeFileSync(destPath, buffer);

    return { filePath: destPath, fileName, size: buffer.length };
  }
}

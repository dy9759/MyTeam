import type {
  Agent,
  AgentRuntime,
  Channel,
  Conversation,
  FileIndex,
  MemberWithUser,
  Message,
  Project,
  ProjectRun,
  ProjectVersion,
  User,
  Workspace,
  WorkspaceAuditEntry,
  WorkspaceSnapshot,
} from "../../../apps/web/shared/types";
import { createLogger, noopLogger, type Logger } from "../../../apps/web/shared/logger";

export class DesktopApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "DesktopApiError";
  }
}

type RequestOptions = RequestInit & {
  silentStatuses?: number[];
};

export class DesktopApiClient {
  private token: string | null = null;
  private workspaceId: string | null = null;
  private readonly logger: Logger;

  constructor(
    private readonly baseUrl: string,
    options?: {
      logger?: Logger;
      onUnauthorized?: () => Promise<void> | void;
    },
  ) {
    this.logger = options?.logger ?? createLogger("desktop-api");
    this.onUnauthorized = options?.onUnauthorized ?? (async () => {});
  }

  private readonly onUnauthorized: () => Promise<void> | void;

  setToken(token: string | null) {
    this.token = token;
  }

  setWorkspaceId(workspaceId: string | null) {
    this.workspaceId = workspaceId;
  }

  private authHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    if (this.token) {
      headers.Authorization = `Bearer ${this.token}`;
    }
    if (this.workspaceId) {
      headers["X-Workspace-ID"] = this.workspaceId;
    }
    return headers;
  }

  private async parseErrorMessage(response: Response, fallback: string) {
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        return body.error;
      }
    } catch {
      // Ignore invalid JSON bodies.
    }
    return fallback;
  }

  private async request<T>(path: string, init?: RequestOptions): Promise<T> {
    const { silentStatuses, ...requestInit } = init ?? {};
    const method = requestInit.method ?? "GET";
    this.logger.info(`→ ${method} ${path}`);

    const response = await fetch(`${this.baseUrl}${path}`, {
      ...requestInit,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...this.authHeaders(),
        ...(requestInit.headers as Record<string, string> | undefined),
      },
    });

    if (!response.ok) {
      if (response.status === 401) {
        await this.onUnauthorized();
      }
      const message = await this.parseErrorMessage(
        response,
        `API error: ${response.status} ${response.statusText}`,
      );
      if (!silentStatuses?.includes(response.status)) {
        this.logger.error(`← ${response.status} ${path}`, { error: message });
      }
      throw new DesktopApiError(message, response.status);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  }

  async getMe(): Promise<User> {
    return this.request("/api/me");
  }

  async listWorkspaces(): Promise<Workspace[]> {
    return this.request("/api/workspaces");
  }

  async listMembers(workspaceId: string): Promise<MemberWithUser[]> {
    return this.request(`/api/workspaces/${workspaceId}/members`);
  }

  async listAgents(params?: {
    workspace_id?: string;
    include_archived?: boolean;
  }): Promise<Agent[]> {
    const search = new URLSearchParams();
    const workspaceId = params?.workspace_id ?? this.workspaceId;
    if (workspaceId) search.set("workspace_id", workspaceId);
    if (params?.include_archived) search.set("include_archived", "true");
    return this.request(`/api/agents?${search.toString()}`);
  }

  async getWorkspaceSnapshot(): Promise<WorkspaceSnapshot> {
    return this.request("/api/workspace/snapshot");
  }

  async listRuntimes(params?: {
    workspace_id?: string;
  }): Promise<AgentRuntime[]> {
    const search = new URLSearchParams();
    const workspaceId = params?.workspace_id ?? this.workspaceId;
    if (workspaceId) search.set("workspace_id", workspaceId);
    return this.request(`/api/runtimes?${search.toString()}`);
  }

  async listProjects(): Promise<Project[]> {
    return this.request("/api/projects");
  }

  async getProject(projectId: string): Promise<Project> {
    return this.request(`/api/projects/${projectId}`);
  }

  async createProject(data: {
    title: string;
    description?: string;
    schedule_type?: string;
  }): Promise<Project> {
    return this.request("/api/projects", {
      method: "POST",
      body: JSON.stringify(data),
    });
  }

  async updateProject(projectId: string, data: Record<string, unknown>): Promise<Project> {
    return this.request(`/api/projects/${projectId}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    });
  }

  async deleteProject(projectId: string): Promise<void> {
    return this.request(`/api/projects/${projectId}`, {
      method: "DELETE",
    });
  }

  async listProjectVersions(projectId: string): Promise<ProjectVersion[]> {
    const response = await this.request<{ versions: ProjectVersion[] } | ProjectVersion[]>(
      `/api/projects/${projectId}/versions`,
    );
    return Array.isArray(response) ? response : response.versions ?? [];
  }

  async listProjectRuns(projectId: string): Promise<ProjectRun[]> {
    const response = await this.request<{ runs: ProjectRun[] } | ProjectRun[]>(
      `/api/projects/${projectId}/runs`,
    );
    return Array.isArray(response) ? response : response.runs ?? [];
  }

  async approvePlan(projectId: string): Promise<void> {
    const project = await this.getProject(projectId);
    const planId = (project as { plan?: { id?: string } }).plan?.id;
    if (!planId) {
      throw new DesktopApiError("project has no plan", 409);
    }
    await this.request(`/api/plans/${planId}/approve`, {
      method: "POST",
    });
  }

  async rejectPlan(projectId: string, reason: string): Promise<void> {
    await this.request(`/api/projects/${projectId}/reject`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
  }

  async listFiles(params?: {
    source_type?: string;
    source_id?: string;
    project_id?: string;
    channel_id?: string;
    owner_id?: string;
    limit?: number;
    offset?: number;
  }): Promise<FileIndex[]> {
    const search = new URLSearchParams();
    if (params?.source_type) search.set("source_type", params.source_type);
    if (params?.source_id) search.set("source_id", params.source_id);
    if (params?.project_id) search.set("project_id", params.project_id);
    if (params?.channel_id) search.set("channel_id", params.channel_id);
    if (params?.owner_id) search.set("owner_id", params.owner_id);
    if (typeof params?.limit === "number") search.set("limit", String(params.limit));
    if (typeof params?.offset === "number") search.set("offset", String(params.offset));
    return this.request(`/api/files?${search.toString()}`);
  }

  async listConversations(): Promise<{ conversations: Conversation[] }> {
    return this.request("/api/messages/conversations");
  }

  async listChannels(): Promise<{ channels: Channel[] }> {
    return this.request("/api/channels");
  }

  async listMessages(params: {
    channel_id?: string;
    recipient_id?: string;
    thread_id?: string;
    limit?: number;
    offset?: number;
  }): Promise<{ messages: Message[] }> {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value != null) {
        search.set(key, String(value));
      }
    }
    return this.request(`/api/messages?${search.toString()}`);
  }

  async getOrCreateSystemAgent(): Promise<{
    id: string;
    name: string;
    agent_type?: string;
  }> {
    return this.request("/api/system-agent");
  }

  async getPersonalAgent(): Promise<Agent> {
    return this.request("/api/personal-agent");
  }

  async sendMessage(params: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: "member" | "agent" | string;
    thread_id?: string;
    content: string;
    content_type?: "text" | "json" | "file" | string;
    parent_message_id?: string;
    file_id?: string;
    file_name?: string;
    file_size?: number;
    file_content_type?: string;
  }): Promise<Message> {
    return this.request<Message>("/api/messages", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }

  async createChannel(params: {
    name: string;
    description?: string;
    visibility?: "public" | "private" | "invite_code";
  }): Promise<Channel> {
    return this.request<Channel>("/api/channels", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }

  async sendTyping(params: {
    channel_id?: string;
    is_typing: boolean;
  }): Promise<void> {
    return this.request("/api/typing", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }

  async listThreadMessages(parentId: string): Promise<{ messages: Message[] }> {
    return this.request(`/api/messages/${parentId}/thread`);
  }

  async listAuditTrail(limit = 50): Promise<WorkspaceAuditEntry[]> {
    const response = await this.request<{ entries: WorkspaceAuditEntry[] }>(
      `/api/audit?limit=${limit}`,
    );
    return response.entries;
  }

  async uploadFile(file: File): Promise<unknown> {
    const formData = new FormData();
    formData.append("file", file);

    const response = await fetch(`${this.baseUrl}/api/upload-file`, {
      method: "POST",
      body: formData,
      headers: this.authHeaders(),
      credentials: "include",
    });

    if (!response.ok) {
      if (response.status === 401) {
        await this.onUnauthorized();
      }
      throw new DesktopApiError(
        await this.parseErrorMessage(response, `Upload failed: ${response.status}`),
        response.status,
      );
    }

    return response.json();
  }
}

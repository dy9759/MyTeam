import { api } from "@/shared/api";

export type MemoryStatus = "candidate" | "confirmed" | "archived";

export type Memory = {
  id: string;
  workspace_id: string;
  type: string;
  scope: string;
  source: string;
  raw: { kind: string; id: string };
  summary?: string;
  body?: string;
  tags: string[];
  entities: string[];
  confidence: number;
  status: MemoryStatus;
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
};

export type Hit = {
  chunk: {
    id: string;
    memory_id: string;
    text: string;
  };
  score: number;
};

export type MemoryFilter = {
  type?: string;
  scope?: string;
  status?: MemoryStatus;
  limit?: number;
  offset?: number;
};

export type CreateMemoryInput = {
  type: string;
  scope: string;
  source: string;
  raw_kind: string;
  raw_id: string;
  summary?: string;
  body?: string;
  tags?: string[];
  entities?: string[];
  confidence?: number;
};

export type SearchInput = {
  query: string;
  top_k?: number;
  type?: string;
  scope?: string;
  status?: MemoryStatus[];
};

type ApiTransport = {
  baseUrl: string;
  authHeaders: () => Record<string, string>;
  handleUnauthorized: () => void;
  parseErrorMessage: (res: Response, fallback: string) => Promise<string>;
};

type ListMemoriesResponse = {
  memories: Memory[];
};

type SearchMemoriesResponse = {
  hits: Hit[];
};

export class MemoryApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "MemoryApiError";
    this.status = status;
  }
}

const transport = api as unknown as ApiTransport;

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method ?? "GET";
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    "X-Request-ID": crypto.randomUUID().slice(0, 8),
    ...transport.authHeaders(),
    ...((init?.headers as Record<string, string>) ?? {}),
  };

  const res = await fetch(`${transport.baseUrl}${path}`, {
    ...init,
    method,
    headers,
    credentials: "include",
  });

  if (!res.ok) {
    if (res.status === 401) transport.handleUnauthorized();
    const message = await transport.parseErrorMessage(
      res,
      `API error: ${res.status} ${res.statusText}`,
    );
    throw new MemoryApiError(message, res.status);
  }

  return res.json() as Promise<T>;
}

export async function listMemories(filter: MemoryFilter = {}): Promise<Memory[]> {
  const search = new URLSearchParams();
  search.set("limit", String(filter.limit ?? 50));
  search.set("offset", String(filter.offset ?? 0));
  if (filter.type) search.set("type", filter.type);
  if (filter.scope) search.set("scope", filter.scope);
  if (filter.status) search.set("status", filter.status);

  const data = await request<ListMemoriesResponse>(`/api/memories?${search}`);
  return data.memories;
}

export async function createMemory(input: CreateMemoryInput): Promise<Memory> {
  return request<Memory>("/api/memories", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function searchMemories(query: SearchInput): Promise<Hit[]> {
  const data = await request<SearchMemoriesResponse>("/api/memories/search", {
    method: "POST",
    body: JSON.stringify(query),
  });
  return data.hits;
}

export async function promoteMemory(id: string): Promise<Memory> {
  return request<Memory>(`/api/memories/${id}/promote`, {
    method: "POST",
  });
}

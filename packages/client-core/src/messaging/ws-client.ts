import type { WSMessage, WSEventType } from "./types";

export type WSStatus =
  | "disconnected"
  | "connecting"
  | "connected"
  | "reconnecting";

type EventHandler = (payload: unknown) => void;
type StatusHandler = (status: WSStatus) => void;

export interface WSClientOptions {
  getToken: () => string | null;
  getWorkspaceId: () => string | null;
  onEvent?: (msg: WSMessage) => void;
  logger?: {
    debug?: (msg: string, ...args: unknown[]) => void;
    info?: (msg: string, ...args: unknown[]) => void;
    warn?: (msg: string, ...args: unknown[]) => void;
  };
}

const BACKOFF_STEPS = [1000, 2000, 4000, 8000, 16000, 30000];

export class WSClient {
  private ws: WebSocket | null = null;
  private baseUrl: string;
  private opts: WSClientOptions;
  private handlers = new Map<WSEventType, Set<EventHandler>>();
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private statusHandlers = new Set<StatusHandler>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private retryIndex = 0;
  private intentionallyClosed = false;
  private statusValue: WSStatus = "disconnected";

  constructor(url: string, opts: WSClientOptions) {
    this.baseUrl = url;
    this.opts = opts;
  }

  get status(): WSStatus {
    return this.statusValue;
  }

  subscribeStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler);
    handler(this.statusValue);
    return () => this.statusHandlers.delete(handler);
  }

  private setStatus(next: WSStatus) {
    if (this.statusValue === next) return;
    this.statusValue = next;
    for (const h of this.statusHandlers) h(next);
  }

  connect() {
    this.intentionallyClosed = false;

    // Cancel any pending reconnect timer from a prior failed attempt.
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    // Close any existing socket to avoid duplicates.
    if (this.ws) {
      const old = this.ws;
      this.ws = null;
      old.onclose = null;
      old.onerror = null;
      old.close();
    }

    this.setStatus(this.retryIndex === 0 ? "connecting" : "reconnecting");

    const url = new URL(this.baseUrl);
    const token = this.opts.getToken();
    const workspaceId = this.opts.getWorkspaceId();
    if (token) url.searchParams.set("token", token);
    if (workspaceId) url.searchParams.set("workspace_id", workspaceId);

    this.ws = new WebSocket(url.toString());

    this.ws.onopen = () => {
      this.setStatus("connected");
      this.opts.logger?.info?.("[ws] connected");
    };

    this.ws.onmessage = (event) => {
      let msg: WSMessage;
      try {
        msg = JSON.parse(event.data as string) as WSMessage;
      } catch {
        this.opts.logger?.warn?.("[ws] bad payload");
        return;
      }
      const handlers = this.handlers.get(msg.type);
      if (handlers) for (const h of handlers) h(msg.payload);
      for (const h of this.anyHandlers) h(msg);
      this.opts.onEvent?.(msg);
    };

    this.ws.onclose = () => {
      this.ws = null;
      if (this.intentionallyClosed) {
        this.setStatus("disconnected");
        return;
      }
      const delay =
        BACKOFF_STEPS[Math.min(this.retryIndex, BACKOFF_STEPS.length - 1)];
      this.retryIndex++;
      this.setStatus("reconnecting");
      this.reconnectTimer = setTimeout(() => this.connect(), delay);
    };

    this.ws.onerror = () => {
      // onclose will follow; no-op here
    };
  }

  disconnect() {
    this.intentionallyClosed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      ws.onclose = null;
      ws.onerror = null;
      ws.close();
    }
    this.retryIndex = 0;
    this.setStatus("disconnected");
  }

  on(event: WSEventType, handler: EventHandler): () => void {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set());
    this.handlers.get(event)!.add(handler);
    return () => this.handlers.get(event)?.delete(handler);
  }

  onAny(handler: (msg: WSMessage) => void): () => void {
    this.anyHandlers.add(handler);
    return () => this.anyHandlers.delete(handler);
  }

  send(message: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }
}

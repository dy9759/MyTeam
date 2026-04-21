package realtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
)

// MembershipChecker verifies a user belongs to a workspace.
type MembershipChecker interface {
	IsMember(ctx context.Context, userID, workspaceID string) bool
}

// PATResolver resolves a Personal Access Token hash to a user ID.
// Nil is acceptable — PAT auth is simply skipped when no resolver is provided.
type PATResolver interface {
	ResolveUserIDFromPATHash(ctx context.Context, hash string) (string, error)
}

// AgentActChecker gates the optional ?agent_id= query param on /ws.
// Returning false makes the upgrader drop agent_id and keep the
// connection user-scoped only (still auth'd against the workspace).
// Nil is acceptable — when no checker is provided, agent_id is
// ignored unconditionally (fail-closed).
type AgentActChecker interface {
	CanActAsAgent(ctx context.Context, userID, agentID, workspaceID string) bool
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Restrict origins in production
		return true
	},
}

// Client represents a single WebSocket connection with identity.
// agentID is optional — set when the connection is attached to a
// specific agent identity (via impersonation / daemon runtime) so the
// agent-interaction layer can do addressed push via SendToAgent.
//
// recentPushes is a small bounded set of interaction IDs that were
// already pushed to this connection. Prevents duplicate delivery on
// reconnect / rapid re-resolve. Ported from AgentmeshHub's
// BoundedUUIDSet pattern (recent 2k ids is enough for typical bursts).
type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	userID       string
	workspaceID  string
	agentID      string
	recentPushes *boundedIDSet
}

// boundedIDSet is a FIFO-capped set of string IDs. Add returns true
// when the id is new; false when already seen (caller should skip).
type boundedIDSet struct {
	mu    sync.Mutex
	cap   int
	order []string
	set   map[string]struct{}
}

func newBoundedIDSet(cap int) *boundedIDSet {
	return &boundedIDSet{
		cap:   cap,
		order: make([]string, 0, cap),
		set:   make(map[string]struct{}, cap),
	}
}

func (b *boundedIDSet) Add(id string) bool {
	if id == "" {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.set[id]; ok {
		return false
	}
	if len(b.order) >= b.cap {
		// Evict oldest
		drop := b.order[0]
		b.order = b.order[1:]
		delete(b.set, drop)
	}
	b.order = append(b.order, id)
	b.set[id] = struct{}{}
	return true
}

// Hub manages WebSocket connections organized by workspace rooms.
type Hub struct {
	rooms      map[string]map[*Client]bool // workspaceID -> clients
	broadcast  chan []byte                  // global broadcast (daemon events)
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub event loop.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			room := client.workspaceID
			if h.rooms[room] == nil {
				h.rooms[room] = make(map[*Client]bool)
			}
			h.rooms[room][client] = true
			total := 0
			for _, r := range h.rooms {
				total += len(r)
			}
			h.mu.Unlock()
			slog.Info("ws client connected", "workspace_id", room, "total_clients", total)

		case client := <-h.unregister:
			h.mu.Lock()
			room := client.workspaceID
			if clients, ok := h.rooms[room]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.send)
					if len(clients) == 0 {
						delete(h.rooms, room)
					}
				}
			}
			total := 0
			for _, r := range h.rooms {
				total += len(r)
			}
			h.mu.Unlock()
			slog.Info("ws client disconnected", "workspace_id", room, "total_clients", total)

		case message := <-h.broadcast:
			// Global broadcast for daemon events (no workspace filtering)
			h.mu.RLock()
			var slow []*Client
			for _, clients := range h.rooms {
				for client := range clients {
					select {
					case client.send <- message:
					default:
						slow = append(slow, client)
					}
				}
			}
			h.mu.RUnlock()
			if len(slow) > 0 {
				h.mu.Lock()
				for _, client := range slow {
					room := client.workspaceID
					if clients, ok := h.rooms[room]; ok {
						if _, exists := clients[client]; exists {
							delete(clients, client)
							close(client.send)
							if len(clients) == 0 {
								delete(h.rooms, room)
							}
						}
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// BroadcastToWorkspace sends a message only to clients in the given workspace.
func (h *Hub) BroadcastToWorkspace(workspaceID string, message []byte) {
	h.mu.RLock()
	clients := h.rooms[workspaceID]
	var slow []*Client
	for client := range clients {
		select {
		case client.send <- message:
		default:
			slow = append(slow, client)
		}
	}
	h.mu.RUnlock()

	// Remove slow clients under write lock
	if len(slow) > 0 {
		h.mu.Lock()
		for _, client := range slow {
			if room, ok := h.rooms[workspaceID]; ok {
				if _, exists := room[client]; exists {
					delete(room, client)
					close(client.send)
					if len(room) == 0 {
						delete(h.rooms, workspaceID)
					}
				}
			}
		}
		h.mu.Unlock()
	}
}

// SendToUser sends a message to all connections belonging to a specific user,
// regardless of which workspace room they are in. Connections in excludeWorkspace
// are skipped (they already receive the message via BroadcastToWorkspace).
func (h *Hub) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}

	h.mu.RLock()
	type target struct {
		client      *Client
		workspaceID string
	}
	var targets []target
	for wsID, clients := range h.rooms {
		if wsID == exclude {
			continue
		}
		for client := range clients {
			if client.userID == userID {
				targets = append(targets, target{client, wsID})
			}
		}
	}
	h.mu.RUnlock()

	var slow []target
	for _, t := range targets {
		select {
		case t.client.send <- message:
		default:
			slow = append(slow, t)
		}
	}

	// Remove slow clients under write lock (same pattern as BroadcastToWorkspace)
	if len(slow) > 0 {
		h.mu.Lock()
		for _, t := range slow {
			if room, ok := h.rooms[t.workspaceID]; ok {
				if _, exists := room[t.client]; exists {
					delete(room, t.client)
					close(t.client.send)
					if len(room) == 0 {
						delete(h.rooms, t.workspaceID)
					}
				}
			}
		}
		h.mu.Unlock()
	}
}

// Broadcast sends a message to all connected clients (used for daemon events).
func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

// SendToAgent pushes a message to every connection whose client.agentID
// matches. No-op when no such client is connected — callers rely on the
// REST inbox endpoint as the pull fallback (mirrors AgentMesh's WS+poll
// resiliency).
//
// When dedupID is non-empty, each target client's bounded recent-push
// set is consulted to skip duplicate delivery. This matters on
// reconnect — an interaction that was already pushed seconds ago
// won't hit the agent twice.
func (h *Hub) SendToAgent(agentID string, dedupID string, message []byte) int {
	if agentID == "" {
		return 0
	}

	h.mu.RLock()
	type target struct {
		client      *Client
		workspaceID string
	}
	var targets []target
	for wsID, clients := range h.rooms {
		for client := range clients {
			if client.agentID == agentID {
				targets = append(targets, target{client, wsID})
			}
		}
	}
	h.mu.RUnlock()

	delivered := 0
	var slow []target
	for _, t := range targets {
		if dedupID != "" && t.client.recentPushes != nil {
			if !t.client.recentPushes.Add(dedupID) {
				// Already pushed to this client, treat as success
				// (AgentMesh semantics — avoid re-sending).
				delivered++
				continue
			}
		}
		select {
		case t.client.send <- message:
			delivered++
		default:
			slow = append(slow, t)
		}
	}

	if len(slow) > 0 {
		h.mu.Lock()
		for _, t := range slow {
			if room, ok := h.rooms[t.workspaceID]; ok {
				if _, exists := room[t.client]; exists {
					delete(room, t.client)
					close(t.client.send)
					if len(room) == 0 {
						delete(h.rooms, t.workspaceID)
					}
				}
			}
		}
		h.mu.Unlock()
	}
	return delivered
}

// PushToAgent marshals msg and delegates to SendToAgent. When msg has
// an `id` field at the top level we use it as the dedup key so the
// bounded recent-push set can suppress duplicates across reconnects.
// Callers without an id can pass msg through SendToAgent directly
// with an empty dedup string.
func (h *Hub) PushToAgent(agentID string, msg any) int {
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}
	// Best-effort dedup key extraction — unmarshal lazily only when
	// msg is a map (the common case: handlers build `map[string]any`).
	dedupID := ""
	if m, ok := msg.(map[string]any); ok {
		if payload, ok := m["payload"].(map[string]any); ok {
			if id, ok := payload["id"].(string); ok {
				dedupID = id
			}
		}
		// interaction envelopes use {type, payload: {id,...}}
		if dedupID == "" {
			if id, ok := m["id"].(string); ok {
				dedupID = id
			}
		}
	}
	return h.SendToAgent(agentID, dedupID, data)
}

// PushToUser sends a message to all connections of a specific user.
func (h *Hub) PushToUser(userID string, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, clients := range h.rooms {
		for client := range clients {
			if client.userID == userID {
				select {
				case client.send <- data:
				default:
				}
			}
		}
	}
}

// PushTyping sends a typing indicator to a workspace.
func (h *Hub) PushTyping(workspaceID string, payload map[string]any) {
	data, _ := json.Marshal(map[string]any{"type": "typing", "payload": payload})
	h.BroadcastToWorkspace(workspaceID, data)
}

// PushPresence broadcasts agent presence change to a workspace.
func (h *Hub) PushPresence(workspaceID string, payload map[string]any) {
	data, _ := json.Marshal(map[string]any{"type": "presence", "payload": payload})
	h.BroadcastToWorkspace(workspaceID, data)
}

// PushSessionUpdate sends session state change to a workspace.
func (h *Hub) PushSessionUpdate(workspaceID string, payload map[string]any) {
	data, _ := json.Marshal(map[string]any{"type": "session:updated", "payload": payload})
	h.BroadcastToWorkspace(workspaceID, data)
}

// HandleWebSocket upgrades an HTTP connection to WebSocket with JWT or PAT auth.
func HandleWebSocket(hub *Hub, mc MembershipChecker, pr PATResolver, ac AgentActChecker, w http.ResponseWriter, r *http.Request) {
	tokenStr := r.URL.Query().Get("token")
	workspaceID := r.URL.Query().Get("workspace_id")
	// Optional: agent_id binds this socket to an agent identity so the
	// message bus can push addressed interactions via SendToAgent.
	// Trusted only after the AgentActChecker says this user can speak
	// as the given agent; without a checker the value is discarded.
	agentID := r.URL.Query().Get("agent_id")

	if tokenStr == "" || workspaceID == "" {
		slog.Warn("ws: missing auth params", "has_token", tokenStr != "", "has_workspace", workspaceID != "")
		http.Error(w, `{"error":"token and workspace_id required"}`, http.StatusUnauthorized)
		return
	}

	var userID string

	if strings.HasPrefix(tokenStr, "mul_") && pr != nil {
		// PAT authentication (desktop clients use PATs stored in keychain).
		h := sha256.Sum256([]byte(tokenStr))
		hash := hex.EncodeToString(h[:])
		uid, err := pr.ResolveUserIDFromPATHash(r.Context(), hash)
		if err != nil {
			slog.Warn("ws: PAT invalid", "error", err)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		userID = uid
	} else {
		// JWT authentication (web clients).
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return auth.JWTSecret(), nil
		})
		if err != nil || !token.Valid {
			slog.Warn("ws: JWT invalid", "error", err)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
			return
		}

		uid, ok := claims["sub"].(string)
		if !ok || strings.TrimSpace(uid) == "" {
			http.Error(w, `{"error":"invalid claims"}`, http.StatusUnauthorized)
			return
		}
		userID = uid
	}

	// Verify user is a member of the workspace
	if !mc.IsMember(r.Context(), userID, workspaceID) {
		http.Error(w, `{"error":"not a member of this workspace"}`, http.StatusForbidden)
		return
	}

	// Gate the agent_id binding. Fail-closed: drop the param if we
	// can't verify the user may act as the named agent, rather than
	// rejecting the whole connection (web clients send this param
	// speculatively and should still get a plain user socket).
	if agentID != "" {
		if ac == nil || !ac.CanActAsAgent(r.Context(), userID, agentID, workspaceID) {
			slog.Warn("ws: agent_id rejected", "user_id", userID, "agent_id", agentID)
			agentID = ""
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		hub:          hub,
		conn:         conn,
		send:         make(chan []byte, 256),
		userID:       userID,
		workspaceID:  workspaceID,
		agentID:      agentID,
		recentPushes: newBoundedIDSet(2000),
	}
	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("websocket read error", "error", err, "user_id", c.userID, "workspace_id", c.workspaceID)
			}
			break
		}
		// TODO: Route inbound messages to appropriate handlers
		slog.Debug("ws message received", "user_id", c.userID, "workspace_id", c.workspaceID)
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			slog.Warn("websocket write error", "error", err)
			return
		}
	}
}

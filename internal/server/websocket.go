// websocket server
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"distributed-editor/internal/fsm"
	"distributed-editor/internal/ot"

	"github.com/gorilla/websocket"
)

// ─────────────────────────────────────────────
// Wire message structs
// ─────────────────────────────────────────────

// message type
type MsgType string

const (
	// Client → Server
	MsgConnect MsgType = "CONNECT"
	MsgSubmit  MsgType = "SUBMIT"

	// Server → Client
	MsgConnectAck MsgType = "CONNECT_ACK"
	MsgAck        MsgType = "ACK"
	MsgBroadcast  MsgType = "BROADCAST"
	MsgRedirect   MsgType = "REDIRECT"
	MsgError      MsgType = "ERROR"
)

// json envelope
type Envelope struct {
	Type    MsgType         `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// connect message
type ConnectMsg struct {
	ClientID     string `json:"client_id"`
	LastKnownRev int    `json:"last_known_rev"` // 0 for new connections
}

// connect ack
type ConnectAckMsg struct {
	HeadText string              `json:"head_text"`
	HeadRev  int                 `json:"head_rev"`
	CatchUp  []fsm.RevisionRecord `json:"catch_up"` // revisions since last_known_rev
}

// submit changeset
type SubmitMsg struct {
	ClientID     string       `json:"client_id"`
	SubmissionID int64        `json:"submission_id"` // monotonically increasing per client
	BaseRev      int          `json:"base_rev"`
	Changeset    ot.Changeset `json:"changeset"`
}

// ack message
type AckMsg struct {
	NewRev int `json:"new_rev"`
}

// broadcast message
type BroadcastMsg struct {
	Changeset ot.Changeset `json:"changeset"`
	NewRev    int          `json:"new_rev"`
}

// redirect message
type RedirectMsg struct {
	LeaderAddr string `json:"leader_addr"` // WebSocket address of the leader
}

// error message
type ErrorMsg struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─────────────────────────────────────────────
// WebSocket upgrader
// ─────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow all origins for development. In production, validate the Origin header.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ─────────────────────────────────────────────
// Client connection
// ─────────────────────────────────────────────

// connected client
type wsClient struct {
	id     string          // client UUID
	conn   *websocket.Conn // underlying WebSocket connection
	sendCh chan []byte      // buffered send channel (written from any goroutine)
	logger *slog.Logger
}

// send message to client
func (c *wsClient) send(msgType MsgType, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		c.logger.Error("failed to marshal payload", "type", msgType, "err", err)
		return
	}
	envelope := Envelope{Type: msgType, Payload: json.RawMessage(data)}
	b, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("failed to marshal envelope", "err", err)
		return
	}
	select {
	case c.sendCh <- b:
	default:
		c.logger.Warn("send channel full, dropping message", "type", msgType, "client_id", c.id)
	}
}

// write loop
func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.sendCh:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.logger.Warn("write failed", "client_id", c.id, "err", err)
				return
			}
		case <-ticker.C:
			// Send WebSocket ping to keep the connection alive.
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─────────────────────────────────────────────
// Node — the main server struct
// ─────────────────────────────────────────────

// server node
type Node struct {
	raftNode *RaftNode
	sm       *fsm.DocumentStateMachine
	logger   *slog.Logger

	// WebSocket leader address (different port from gRPC).
	// Announced in REDIRECT messages so clients know where to reconnect.
	wsLeaderAddr string

	// Connected clients, keyed by client_id.
	mu      sync.RWMutex
	clients map[string]*wsClient
}

// create server node
func NewNode(rn *RaftNode, sm *fsm.DocumentStateMachine, wsLeaderAddr string, logger *slog.Logger) *Node {
	n := &Node{
		raftNode:     rn,
		sm:           sm,
		logger:       logger,
		wsLeaderAddr: wsLeaderAddr,
		clients:      make(map[string]*wsClient),
	}

	// Register the FSM callback — called on EVERY NODE whenever an entry commits.
	sm.SetOnCommit(n.onCommit)
	return n
}

// handle http request
func (n *Node) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		n.logger.Warn("websocket upgrade failed", "err", err)
		return
	}
	// readPump handles the connection lifecycle.
	go n.readPump(conn)
}

// ─────────────────────────────────────────────
// readPump — per-connection goroutine
// ─────────────────────────────────────────────

// read loop
func (n *Node) readPump(conn *websocket.Conn) {
	var client *wsClient

	defer func() {
		if client != nil {
			n.removeClient(client.id)
			close(client.sendCh)
		}
		conn.Close()
	}()

	conn.SetReadLimit(1 << 20) // 1 MB max message
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				n.logger.Warn("websocket closed unexpectedly", "err", err)
			}
			return
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			n.logger.Warn("invalid envelope", "err", err)
			continue
		}

		switch env.Type {
		case MsgConnect:
			var msg ConnectMsg
			if err := json.Unmarshal(env.Payload, &msg); err != nil {
				break
			}
			client = n.handleConnect(conn, msg)

		case MsgSubmit:
			if client == nil {
				break // must CONNECT first
			}
			var msg SubmitMsg
			if err := json.Unmarshal(env.Payload, &msg); err != nil {
				break
			}
			n.handleSubmit(client, msg)

		default:
			n.logger.Warn("unknown message type", "type", env.Type)
		}
	}
}

// ─────────────────────────────────────────────
// handleConnect
// ─────────────────────────────────────────────

// process connect
func (n *Node) handleConnect(conn *websocket.Conn, msg ConnectMsg) *wsClient {
	client := &wsClient{
		id:     msg.ClientID,
		conn:   conn,
		sendCh: make(chan []byte, 256),
		logger: n.logger.With("client_id", msg.ClientID),
	}

	// Start the write pump goroutine for this client.
	go client.writePump()

	// Register the client.
	n.mu.Lock()
	n.clients[msg.ClientID] = client
	n.mu.Unlock()

	// ── Leader check ──
	// If we are not the leader, redirect immediately so the client doesn't
	// start typing on a node that can't accept writes.
	if !n.raftNode.IsLeader() {
		leaderAddr := n.raftNode.LeaderAddr()
		if leaderAddr != "" {
			n.logger.Info("eager redirect on connect", "client_id", msg.ClientID, "leader_addr", leaderAddr)
			client.send(MsgRedirect, RedirectMsg{LeaderAddr: leaderAddr})
			return client
		}
	}

	// Build catch-up: all revisions since last_known_rev.
	catchUp := n.sm.RevisionsSince(msg.LastKnownRev)

	// Send the CONNECT_ACK.
	client.send(MsgConnectAck, ConnectAckMsg{
		HeadText: n.sm.HeadText(),
		HeadRev:  n.sm.HeadRev(),
		CatchUp:  catchUp,
	})

	n.logger.Info("client connected",
		"client_id", msg.ClientID,
		"last_known_rev", msg.LastKnownRev,
		"head_rev", n.sm.HeadRev(),
		"catch_up_count", len(catchUp),
	)

	return client
}

// ─────────────────────────────────────────────
// handleSubmit
// ─────────────────────────────────────────────

// process submit
func (n *Node) handleSubmit(client *wsClient, msg SubmitMsg) {
	// ── Leader check ──────────────────────────────────────────────────────────
	if !n.raftNode.IsLeader() {
		leaderAddr := n.raftNode.LeaderAddr()
		n.logger.Info("redirecting client to leader",
			"client_id", client.id,
			"leader_addr", leaderAddr,
		)
		client.send(MsgRedirect, RedirectMsg{LeaderAddr: leaderAddr})
		return
	}

	// ── Validate the incoming changeset ───────────────────────────────────────
	if err := msg.Changeset.Validate(); err != nil {
		n.logger.Warn("invalid changeset from client",
			"client_id", client.id,
			"err", err,
		)
		client.send(MsgError, ErrorMsg{Code: 400, Message: "invalid changeset: " + err.Error()})
		return
	}

	n.logger.Info("received submit",
		"client_id", client.id,
		"submission_id", msg.SubmissionID,
		"base_rev", msg.BaseRev,
	)

	// ── Propose to Raft ───────────────────────────────────────────────────────
	// Encode the raw entry (un-transformed changeset) as a Raft log payload.
	entry := fsm.RaftLogEntry{
		ClientID:     msg.ClientID,
		SubmissionID: msg.SubmissionID,
		BaseRev:      msg.BaseRev,
		Changeset:    msg.Changeset,
	}
	data, err := fsm.MarshalEntry(entry)
	if err != nil {
		client.send(MsgError, ErrorMsg{Code: 500, Message: "failed to marshal entry"})
		return
	}

	// Apply is asynchronous — Raft replicates to a majority before committing.
	// Timeout for the apply operation.
	future := n.raftNode.Raft.Apply(data, 5*time.Second)
	if err := future.Error(); err != nil {
		n.logger.Error("raft apply failed", "client_id", client.id, "err", err)
		client.send(MsgError, ErrorMsg{Code: 500, Message: "raft apply failed: " + err.Error()})
		return
	}

	// The FSM.Apply() return value is *fsm.ApplyResult (or nil for duplicates).
	// The ACK + Broadcast are handled by the onCommit callback registered in NewNode,
	// so we don't need to do anything else here.
}

// ─────────────────────────────────────────────
// onCommit — FSM callback
// ─────────────────────────────────────────────

// commit callback
func (n *Node) onCommit(result fsm.ApplyResult) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// Send ACK to the originating client (if connected to THIS server).
	if origin, ok := n.clients[result.ClientID]; ok {
		origin.send(MsgAck, AckMsg{NewRev: result.NewRev})
	}

	// If this was a duplicate, we don't broadcast anything because no change occurred.
	if result.IsDuplicate {
		return
	}

	// Broadcast the transformed changeset C' to all OTHER locally connected clients.
	for cid, client := range n.clients {
		if cid == result.ClientID {
			continue
		}
		client.send(MsgBroadcast, BroadcastMsg{
			Changeset: result.CPrime,
			NewRev:    result.NewRev,
		})
	}

	n.logger.Info("committed and broadcast",
		"author", result.ClientID,
		"new_rev", result.NewRev,
		"client_count", len(n.clients),
	)
}

// ─────────────────────────────────────────────
// removeClient
// ─────────────────────────────────────────────

// remove client
func (n *Node) removeClient(clientID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.clients, clientID)
	n.logger.Info("client disconnected", "client_id", clientID)
}

// ConnectedClients returns the number of clients currently connected to this node.
func (n *Node) ConnectedClients() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.clients)
}

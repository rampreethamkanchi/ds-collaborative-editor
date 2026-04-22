// network layer
package raft

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// msg types
const (
	msgAppendEntriesReq  byte = 0
	msgAppendEntriesResp byte = 1
	msgRequestVoteReq    byte = 2
	msgRequestVoteResp   byte = 3
)

// transport struct
type TCPTransport struct {
	localAddr string       // the address we listen on
	listener  net.Listener // TCP listener for incoming connections
	rpcCh     chan *RPC     // incoming RPCs are placed here for the Raft node

	shutdownCh chan struct{}
	shutdownMu sync.Mutex
	shutdown   bool

	logger *slog.Logger
}

// create transport
func NewTCPTransport(bindAddr string, logger *slog.Logger) (*TCPTransport, error) {
	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("tcp_transport: listen on %s: %w", bindAddr, err)
	}

	t := &TCPTransport{
		localAddr:  bindAddr,
		listener:   listener,
		rpcCh:      make(chan *RPC, 256),
		shutdownCh: make(chan struct{}),
		logger:     logger,
	}

	// Start accepting incoming connections.
	go t.acceptLoop()

	return t, nil
}

// get rpc channel
func (t *TCPTransport) Consumer() <-chan *RPC {
	return t.rpcCh
}

// get address
func (t *TCPTransport) LocalAddr() string {
	return t.localAddr
}

// send rpcs

// send append request
func (t *TCPTransport) SendAppendEntries(target string, req *AppendEntriesRequest) (*AppendEntriesResponse, error) {
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("tcp_transport: dial %s: %w", target, err)
	}
	defer conn.Close()

	if err := t.sendMessage(conn, msgAppendEntriesReq, req); err != nil {
		return nil, fmt.Errorf("send AppendEntries to %s: %w", target, err)
	}

	var resp AppendEntriesResponse
	if err := t.readMessage(conn, msgAppendEntriesResp, &resp); err != nil {
		return nil, fmt.Errorf("read AppendEntries response from %s: %w", target, err)
	}

	return &resp, nil
}

// send vote request
func (t *TCPTransport) SendRequestVote(target string, req *RequestVoteRequest) (*RequestVoteResponse, error) {
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("tcp_transport: dial %s: %w", target, err)
	}
	defer conn.Close()

	if err := t.sendMessage(conn, msgRequestVoteReq, req); err != nil {
		return nil, fmt.Errorf("send RequestVote to %s: %w", target, err)
	}

	var resp RequestVoteResponse
	if err := t.readMessage(conn, msgRequestVoteResp, &resp); err != nil {
		return nil, fmt.Errorf("read RequestVote response from %s: %w", target, err)
	}

	return &resp, nil
}

// listen for connections
func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.shutdownCh:
				return
			default:
				t.logger.Warn("tcp_transport: accept error", "err", err)
				continue
			}
		}
		go t.handleConn(conn)
	}
}

// read requests
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	for {
		header := make([]byte, 5)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}

		msgType := header[0]
		payloadLen := binary.BigEndian.Uint32(header[1:5])

		if payloadLen > 10*1024*1024 {
			t.logger.Warn("tcp_transport: payload too large", "len", payloadLen)
			return
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}

		switch msgType {
		case msgAppendEntriesReq:
			var req AppendEntriesRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				t.logger.Warn("tcp_transport: unmarshal AppendEntries", "err", err)
				return
			}
			respCh := make(chan RPCResponse, 1)
			t.rpcCh <- &RPC{Command: &req, RespCh: respCh}
			
			select {
			case rpcResp := <-respCh:
				if err := t.sendMessage(conn, msgAppendEntriesResp, rpcResp.Response); err != nil {
					return
				}
			case <-time.After(10 * time.Second):
				t.logger.Warn("tcp_transport: Raft node processing timeout (AppendEntries)")
				return
			case <-t.shutdownCh:
				return
			}

		case msgRequestVoteReq:
			var req RequestVoteRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				t.logger.Warn("tcp_transport: unmarshal RequestVote", "err", err)
				return
			}
			respCh := make(chan RPCResponse, 1)
			t.rpcCh <- &RPC{Command: &req, RespCh: respCh}
			
			select {
			case rpcResp := <-respCh:
				if err := t.sendMessage(conn, msgRequestVoteResp, rpcResp.Response); err != nil {
					return
				}
			case <-time.After(10 * time.Second):
				t.logger.Warn("tcp_transport: Raft node processing timeout (RequestVote)")
				return
			case <-t.shutdownCh:
				return
			}

		default:
			t.logger.Warn("tcp_transport: unknown message type", "type", msgType)
			return
		}
		
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
}

// write logic
func (t *TCPTransport) sendMessage(w io.Writer, msgType byte, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	msg := make([]byte, 5+len(body))
	msg[0] = msgType
	binary.BigEndian.PutUint32(msg[1:5], uint32(len(body)))
	copy(msg[5:], body)

	if conn, ok := w.(net.Conn); ok {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	}
	
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	return nil
}

// read logic
func (t *TCPTransport) readMessage(r io.Reader, expectedType byte, out interface{}) error {
	if conn, ok := r.(net.Conn); ok {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	}

	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	msgType := header[0]
	if msgType != expectedType {
		return fmt.Errorf("expected message type %d, got %d", expectedType, msgType)
	}

	payloadLen := binary.BigEndian.Uint32(header[1:5])
	if payloadLen > 10*1024*1024 {
		return fmt.Errorf("payload too large: %d", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	return json.Unmarshal(payload, out)
}

// shutdown transport
func (t *TCPTransport) Shutdown() error {
	t.shutdownMu.Lock()
	defer t.shutdownMu.Unlock()

	if t.shutdown {
		return nil
	}
	t.shutdown = true
	close(t.shutdownCh)

	// Close the listener so acceptLoop returns.
	t.listener.Close()

	return nil
}

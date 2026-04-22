// test helpers
package raft

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// memory log store
type InmemLogStore struct {
	mu      sync.RWMutex
	entries map[uint64]*LogEntry
	lowIdx  uint64
	highIdx uint64
}

// constructor
func NewInmemLogStore() *InmemLogStore {
	return &InmemLogStore{
		entries: make(map[uint64]*LogEntry),
	}
}

func (s *InmemLogStore) FirstIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lowIdx, nil
}

func (s *InmemLogStore) LastIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highIdx, nil
}

func (s *InmemLogStore) GetLog(index uint64) (*LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[index]
	if !ok {
		return nil, fmt.Errorf("log entry %d not found", index)
	}
	cp := *entry
	cp.Data = make([]byte, len(entry.Data))
	copy(cp.Data, entry.Data)
	return &cp, nil
}

func (s *InmemLogStore) StoreLog(entry *LogEntry) error {
	return s.StoreLogs([]*LogEntry{entry})
}

func (s *InmemLogStore) StoreLogs(entries []*LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		cp := *e
		cp.Data = make([]byte, len(e.Data))
		copy(cp.Data, e.Data)
		s.entries[e.Index] = &cp

		if s.lowIdx == 0 || e.Index < s.lowIdx {
			s.lowIdx = e.Index
		}
		if e.Index > s.highIdx {
			s.highIdx = e.Index
		}
	}
	return nil
}

func (s *InmemLogStore) DeleteRange(min, max uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := min; i <= max; i++ {
		delete(s.entries, i)
	}
	s.lowIdx = 0
	s.highIdx = 0
	for idx := range s.entries {
		if s.lowIdx == 0 || idx < s.lowIdx {
			s.lowIdx = idx
		}
		if idx > s.highIdx {
			s.highIdx = idx
		}
	}
	return nil
}

// memory stable store
type InmemStableStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// constructor
func NewInmemStableStore() *InmemStableStore {
	return &InmemStableStore{
		data: make(map[string][]byte),
	}
}

func (s *InmemStableStore) Set(key []byte, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(val))
	copy(cp, val)
	s.data[string(key)] = cp
	return nil
}

func (s *InmemStableStore) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[string(key)]
	if !ok {
		return nil, nil
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, nil
}

func (s *InmemStableStore) SetUint64(key []byte, val uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, val)
	return s.Set(key, buf)
}

func (s *InmemStableStore) GetUint64(key []byte) (uint64, error) {
	val, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	if len(val) == 0 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(val), nil
}

// memory transport
type InmemTransport struct {
	localAddr string
	rpcCh     chan *RPC
	mu        sync.RWMutex
	peers     map[string]*InmemTransport
	shutdown  bool
}

// constructor
func NewInmemTransport(addr string) *InmemTransport {
	return &InmemTransport{
		localAddr: addr,
		rpcCh:     make(chan *RPC, 256),
		peers:     make(map[string]*InmemTransport),
	}
}

// rpc consumer
func (t *InmemTransport) Consumer() <-chan *RPC {
	return t.rpcCh
}

func (t *InmemTransport) LocalAddr() string {
	return t.localAddr
}

// add peer
func (t *InmemTransport) Connect(addr string, peer *InmemTransport) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers[addr] = peer
}

// remove peer
func (t *InmemTransport) Disconnect(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.peers, addr)
}

// remove all peers
func (t *InmemTransport) DisconnectAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers = make(map[string]*InmemTransport)
}

// send append request
func (t *InmemTransport) SendAppendEntries(target string, req *AppendEntriesRequest) (*AppendEntriesResponse, error) {
	t.mu.RLock()
	peer, ok := t.peers[target]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("inmem_transport: peer %q not connected", target)
	}

	respCh := make(chan RPCResponse, 1)
	rpc := &RPC{Command: req, RespCh: respCh}

	select {
	case peer.rpcCh <- rpc:
	case <-time.After(1 * time.Second):
		return nil, fmt.Errorf("inmem_transport: timeout sending AppendEntries to %s", target)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Response.(*AppendEntriesResponse), nil
	case <-time.After(1 * time.Second):
		return nil, fmt.Errorf("inmem_transport: timeout waiting for AppendEntries response from %s", target)
	}
}

// send vote request
func (t *InmemTransport) SendRequestVote(target string, req *RequestVoteRequest) (*RequestVoteResponse, error) {
	t.mu.RLock()
	peer, ok := t.peers[target]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("inmem_transport: peer %q not connected", target)
	}

	respCh := make(chan RPCResponse, 1)
	rpc := &RPC{Command: req, RespCh: respCh}

	select {
	case peer.rpcCh <- rpc:
	case <-time.After(1 * time.Second):
		return nil, fmt.Errorf("inmem_transport: timeout sending RequestVote to %s", target)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Response.(*RequestVoteResponse), nil
	case <-time.After(1 * time.Second):
		return nil, fmt.Errorf("inmem_transport: timeout waiting for RequestVote response from %s", target)
	}
}

func (t *InmemTransport) Shutdown() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shutdown = true
	t.peers = make(map[string]*InmemTransport)
	return nil
}

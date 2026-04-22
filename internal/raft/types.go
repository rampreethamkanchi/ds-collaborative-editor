// raft types
package raft

import (
	"errors"
	"fmt"
)

var ErrLogNotFound = errors.New("raft: log entry not found")

// server roles
type ServerState int

const (
	Follower  ServerState = iota // passive node
	Candidate                    // seeking election
	Leader                       // current leader
)

// String returns a human-readable name for the server state.
func (s ServerState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// log record
type LogEntry struct {
	Index uint64 // index in log
	Term  uint64 // term number
	Data  []byte // actual command
}

// append entries rpc
type AppendEntriesRequest struct {
	Term         uint64     `json:"term"`           // current term
	LeaderID     string     `json:"leader_id"`      // leader id
	PrevLogIndex uint64     `json:"prev_log_index"` // previous index
	PrevLogTerm  uint64     `json:"prev_log_term"`  // previous term
	Entries      []LogEntry `json:"entries"`        // entries to sync
	LeaderCommit uint64     `json:"leader_commit"`  // leader commit index
}

// append response
type AppendEntriesResponse struct {
	Term    uint64 `json:"term"`    // current term
	Success bool   `json:"success"` // true if accepted

	ConflictTerm  uint64 `json:"conflict_term"`  // conflict term
	ConflictIndex uint64 `json:"conflict_index"` // conflict index
}

// vote request
type RequestVoteRequest struct {
	Term         uint64 `json:"term"`           // candidate term
	CandidateID  string `json:"candidate_id"`   // candidate id
	LastLogIndex uint64 `json:"last_log_index"` // last index
	LastLogTerm  uint64 `json:"last_log_term"`  // last term
}

// vote response
type RequestVoteResponse struct {
	Term        uint64 `json:"term"`         // current term
	VoteGranted bool   `json:"vote_granted"` // true if granted
}

// internal rpc struct
type RPC struct {
	Command interface{}      // request payload
	RespCh  chan RPCResponse // response channel
}

// rpc response wrapper
type RPCResponse struct {
	Response interface{}
	Error    error
}

// Respond is a helper to send a response on the RPC's response channel.
func (r *RPC) Respond(resp interface{}, err error) {
	r.RespCh <- RPCResponse{Response: resp, Error: err}
}

// state machine interface
type FSM interface {
	Apply(data []byte) interface{} // apply to fsm
}

// log storage interface
type LogStore interface {
	FirstIndex() (uint64, error)
	LastIndex() (uint64, error)
	GetLog(index uint64) (*LogEntry, error)
	StoreLog(entry *LogEntry) error
	StoreLogs(entries []*LogEntry) error
	DeleteRange(min, max uint64) error
}

// persistent state interface
type StableStore interface {
	Set(key []byte, val []byte) error
	Get(key []byte) ([]byte, error)
	SetUint64(key []byte, val uint64) error
	GetUint64(key []byte) (uint64, error)
}

// saved keys
var (
	KeyCurrentTerm = []byte("CurrentTerm")
	KeyVotedFor    = []byte("VotedFor")
)

// network transport interface
type Transport interface {
	Consumer() <-chan *RPC // rpc source
	LocalAddr() string      // listen address
	
	SendAppendEntries(target string, req *AppendEntriesRequest) (*AppendEntriesResponse, error)
	SendRequestVote(target string, req *RequestVoteRequest) (*RequestVoteResponse, error)
	
	Shutdown() error
}

// peer config
type PeerConfig struct {
	ID      string
	Address string
}

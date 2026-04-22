// config logic
package raft

import (
	"log/slog"
	"time"
)

// raft config struct
type Config struct {
	ServerID string // server id

	// cluster peers
	Peers []PeerConfig

	HeartbeatInterval time.Duration // interval for heartbeat

	// timeout range
	ElectionTimeoutMin time.Duration
	ElectionTimeoutMax time.Duration

	LogStore    LogStore    // log storage
	StableStore StableStore // persistent storage
	FSM         FSM         // state machine

	Logger *slog.Logger // logger
}

// get default config
func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval: 150 * time.Millisecond,

		ElectionTimeoutMin: 1000 * time.Millisecond,
		ElectionTimeoutMax: 2000 * time.Millisecond,
	}
}

// validate fields
func (c *Config) validate() error {
	if c.ServerID == "" {
		return ErrConfigMissingField("ServerID")
	}
	if len(c.Peers) == 0 {
		return ErrConfigMissingField("Peers")
	}
	if c.LogStore == nil {
		return ErrConfigMissingField("LogStore")
	}
	if c.StableStore == nil {
		return ErrConfigMissingField("StableStore")
	}
	if c.FSM == nil {
		return ErrConfigMissingField("FSM")
	}
	if c.HeartbeatInterval <= 0 {
		return ErrConfigMissingField("HeartbeatInterval")
	}
	if c.ElectionTimeoutMin <= 0 || c.ElectionTimeoutMax <= c.ElectionTimeoutMin {
		return ErrConfigMissingField("ElectionTimeout (invalid range)")
	}
	
	// verify server in peer list
	found := false
	for _, p := range c.Peers {
		if p.ID == c.ServerID {
			found = true
			break
		}
	}
	if !found {
		return ErrConfigMissingField("ServerID not found in Peers list")
	}
	return nil
}

// config error
type ErrConfigMissingField string

func (e ErrConfigMissingField) Error() string {
	return "raft: missing or invalid config field: " + string(e)
}

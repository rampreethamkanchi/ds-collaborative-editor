// async result logic
package raft

import (
	"errors"
	"sync"
)

// raft errors
var (
	ErrNotLeader  = errors.New("raft: not the leader")
	ErrShutdown   = errors.New("raft: server is shutting down")
	ErrTimeout    = errors.New("raft: apply timeout")
	ErrLeaderLost = errors.New("raft: leadership lost before commit")
)

// async result struct
type ApplyFuture struct {
	errCh    chan error  // channel for error
	response interface{} // fsm response

	mu          sync.Mutex
	respondOnce sync.Once
	readOnce    sync.Once
	err         error
}

// constructor
func newApplyFuture() *ApplyFuture {
	return &ApplyFuture{
		errCh: make(chan error, 1),
	}
}

// wait for result
func (f *ApplyFuture) Error() error {
	f.readOnce.Do(func() {
		f.err = <-f.errCh
	})
	return f.err
}

// get fsm response
func (f *ApplyFuture) Response() interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.response
}

// set result
func (f *ApplyFuture) respond(err error, response interface{}) {
	f.respondOnce.Do(func() {
		f.mu.Lock()
		f.response = response
		f.mu.Unlock()
		f.errCh <- err
	})
}

// apply request internal message
type applyRequest struct {
	data   []byte
	future *ApplyFuture
}

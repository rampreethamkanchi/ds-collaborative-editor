// election logic
package raft

import (
	"sync"
)

// ─────────────────────────────────────────────────────────────────────────────
// Starting an election (Candidate side)
// ─────────────────────────────────────────────────────────────────────────────

// start voting process
func (r *RaftNode) startElection() <-chan RequestVoteResponse {
	r.mu.Lock()

	r.currentTerm++ // increment term

	r.votedFor = r.config.ServerID // vote for self

	r.persistState() // save state

	// Capture current state for the RPC (read under lock, send without lock).
	term := r.currentTerm
	candidateID := r.config.ServerID
	lastLogIndex, lastLogTerm := r.lastLogInfo()

	r.logger.Info("starting election",
		"term", term,
		"last_log_index", lastLogIndex,
		"last_log_term", lastLogTerm,
	)

	r.mu.Unlock()

	// send vote rpcs
	req := &RequestVoteRequest{
		Term:         term,
		CandidateID:  candidateID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	// Buffered channel large enough for all peer responses.
	voteCh := make(chan RequestVoteResponse, len(r.config.Peers))

	var wg sync.WaitGroup
	for _, peer := range r.config.Peers {
		if peer.ID == r.config.ServerID {
			continue // don't send to ourselves
		}
		wg.Add(1)
		go func(peerAddr string) {
			defer wg.Done()
			resp, err := r.transport.SendRequestVote(peerAddr, req)
			if err != nil {
				r.logger.Debug("RequestVote RPC failed",
					"peer", peerAddr,
					"err", err,
				)
				return // network error — this vote is lost
			}
			voteCh <- *resp
		}(peer.Address)
	}

	// Close the channel when all RPCs complete (so the caller's range loop ends).
	go func() {
		wg.Wait()
		close(voteCh)
	}()

	return voteCh
}

// ─────────────────────────────────────────────────────────────────────────────
// Handling incoming RequestVote (Voter side)
// ─────────────────────────────────────────────────────────────────────────────

// handle incoming vote request
func (r *RaftNode) handleRequestVote(req *RequestVoteRequest) (*RequestVoteResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	resp := &RequestVoteResponse{
		Term:        r.currentTerm,
		VoteGranted: false,
	}

	if req.Term < r.currentTerm {
		r.logger.Debug("rejecting RequestVote: stale term",
			"candidate", req.CandidateID,
			"candidate_term", req.Term,
			"our_term", r.currentTerm,
		)
		return resp, false
	}

	if req.Term > r.currentTerm {
		r.stepDown(req.Term)
	}

	resp.Term = r.currentTerm

	if r.votedFor != "" && r.votedFor != req.CandidateID {
		r.logger.Debug("rejecting RequestVote: already voted",
			"candidate", req.CandidateID,
			"voted_for", r.votedFor,
		)
		return resp, false
	}

	myLastIndex, myLastTerm := r.lastLogInfo()
	if !isLogUpToDate(req.LastLogTerm, req.LastLogIndex, myLastTerm, myLastIndex) {
		r.logger.Debug("rejecting RequestVote: candidate log not up-to-date",
			"candidate", req.CandidateID,
			"candidate_last_term", req.LastLogTerm,
			"candidate_last_index", req.LastLogIndex,
			"our_last_term", myLastTerm,
			"our_last_index", myLastIndex,
		)
		return resp, false
	}

	r.votedFor = req.CandidateID
	r.persistState()

	resp.VoteGranted = true
	r.logger.Info("granted vote",
		"candidate", req.CandidateID,
		"term", r.currentTerm,
	)

	// Return true to signal the caller to reset the election timer.
	// (Granting a vote is one of the events that resets the timer.)
	return resp, true
}

// ─────────────────────────────────────────────────────────────────────────────
// Log up-to-date comparison (§5.4.1)
// ─────────────────────────────────────────────────────────────────────────────

// helper to compare logs
func isLogUpToDate(candidateLastTerm, candidateLastIndex, voterLastTerm, voterLastIndex uint64) bool {
	if candidateLastTerm > voterLastTerm {
		return true
	}
	if candidateLastTerm == voterLastTerm && candidateLastIndex >= voterLastIndex {
		return true
	}
	return false
}

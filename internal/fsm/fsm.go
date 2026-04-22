// state machine logic
package fsm

import (
	"log/slog"
	"sync"

	"distributed-editor/internal/ot"
)

// fsm struct
type DocumentStateMachine struct {
	mu sync.RWMutex

	headText string // current text
	headRev  int    // revision number

	// log of changes
	revisionLog []RevisionRecord

	// for deduplication
	clientLastSubmission map[string]int64

	logger *slog.Logger

	// commit callback
	onCommit func(result ApplyResult)
}

// constructor
func NewDocumentStateMachine(initialText string, logger *slog.Logger) *DocumentStateMachine {
	return &DocumentStateMachine{
		headText:             initialText,
		headRev:              0,
		revisionLog:          make([]RevisionRecord, 0),
		clientLastSubmission: make(map[string]int64),
		logger:               logger,
		onCommit:             func(ApplyResult) {},
	}
}

// set callback
func (fsm *DocumentStateMachine) SetOnCommit(cb func(ApplyResult)) {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()
	fsm.onCommit = cb
}

// apply logic for raft
func (fsm *DocumentStateMachine) Apply(data []byte) interface{} {
	// decode entry
	entry, err := UnmarshalEntry(data)
	if err != nil {
		fsm.logger.Error("failed to unmarshal log entry", "err", err)
		return nil
	}

	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	// check if already done
	lastSub, seen := fsm.clientLastSubmission[entry.ClientID]
	if seen && entry.SubmissionID <= lastSub {
		fsm.logger.Info("skipping duplicate entry",
			"client_id", entry.ClientID,
			"submission_id", entry.SubmissionID,
			"last_applied", lastSub,
		)
		res := &ApplyResult{
			ClientID:    entry.ClientID,
			NewRev:      fsm.headRev,
			IsDuplicate: true,
		}
		go func() {
			fsm.onCommit(*res)
		}()
		return res
	}

	// ot part
	C := entry.Changeset

	for r := entry.BaseRev; r < fsm.headRev; r++ {
		historical := fsm.revisionLog[r].Changeset
		C, err = ot.Follow(historical, C)
		if err != nil {
			fsm.logger.Error("OT follow failed",
				"rev", r, "client_id", entry.ClientID, "err", err)
			return nil
		}
	}

	// update document
	newText, err := ot.ApplyChangeset(fsm.headText, C)
	if err != nil {
		fsm.logger.Error("apply_changeset failed",
			"client_id", entry.ClientID,
			"head_rev", fsm.headRev,
			"changeset", C,
			"err", err,
		)
		return nil
	}

	// save revision
	record := RevisionRecord{
		RevNumber: fsm.headRev,
		Changeset: C,
		Source:    entry.ClientID,
	}
	fsm.revisionLog = append(fsm.revisionLog, record)

	fsm.headText = newText
	fsm.headRev++
	fsm.clientLastSubmission[entry.ClientID] = entry.SubmissionID

	fsm.logger.Info("applied changeset",
		"client_id", entry.ClientID,
		"base_rev", entry.BaseRev,
		"new_rev", fsm.headRev,
		"new_len", len(newText),
	)

	// call callback
	result := ApplyResult{
		CPrime:   C,
		NewRev:   fsm.headRev,
		ClientID: entry.ClientID,
	}

	go func() {
		fsm.onCommit(result)
	}()

	return &result
}

// get current text
func (fsm *DocumentStateMachine) HeadText() string {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	return fsm.headText
}

// get revision
func (fsm *DocumentStateMachine) HeadRev() int {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	return fsm.headRev
}

// get missed revisions
func (fsm *DocumentStateMachine) RevisionsSince(fromRev int) []RevisionRecord {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	if fromRev >= len(fsm.revisionLog) {
		return nil
	}
	slice := fsm.revisionLog[fromRev:]
	result := make([]RevisionRecord, len(slice))
	copy(result, slice)
	return result
}

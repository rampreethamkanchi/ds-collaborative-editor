// fsm types
package fsm

import (
	"distributed-editor/internal/ot"
	"encoding/json"
)

// entry for raft log
type RaftLogEntry struct {
	ClientID     string        `json:"client_id"`
	SubmissionID int64         `json:"submission_id"`
	BaseRev      int           `json:"base_rev"`
	Changeset    ot.Changeset  `json:"changeset"`
}

// history record
type RevisionRecord struct {
	RevNumber int          `json:"rev_number"`
	Changeset ot.Changeset `json:"changeset"`
	Source    string       `json:"source"`
}

// result after applying
type ApplyResult struct {
	CPrime      ot.Changeset
	NewRev      int
	ClientID    string
	IsDuplicate bool
}

// encode to json
func MarshalEntry(e RaftLogEntry) ([]byte, error) {
	return json.Marshal(e)
}

// decode from json
func UnmarshalEntry(data []byte) (RaftLogEntry, error) {
	var e RaftLogEntry
	err := json.Unmarshal(data, &e)
	return e, err
}

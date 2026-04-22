// OT implementation for EasySync
package ot

import (
	"encoding/json"
	"fmt"
)

// operation types
type OpType string

const (
	OpRetain OpType = "retain" // keep as it is
	OpInsert OpType = "insert" // new text
	OpDelete OpType = "delete" // remove text
)

// single operation
type Op struct {
	Type  OpType `json:"op"`
	Len   int    `json:"n,omitempty"`    // for retain and delete
	Chars string `json:"chars,omitempty"` // for insert
}

// document changeset
type Changeset struct {
	OldLen int  `json:"old_len"`
	NewLen int  `json:"new_len"`
	Ops    []Op `json:"ops"`
}

// print changeset
func (c Changeset) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// creates identity changeset
func Identity(n int) Changeset {
	if n == 0 {
		return Changeset{OldLen: 0, NewLen: 0, Ops: []Op{}}
	}
	return Changeset{
		OldLen: n,
		NewLen: n,
		Ops:    []Op{{Type: OpRetain, Len: n}},
	}
}

// check if it is identity
func IsIdentity(c Changeset) bool {
	if c.OldLen != c.NewLen {
		return false
	}
	if c.OldLen == 0 && len(c.Ops) == 0 {
		return true
	}
	if len(c.Ops) == 1 && c.Ops[0].Type == OpRetain && c.Ops[0].Len == c.OldLen {
		return true
	}
	return false
}

// text to changeset conversion
func TextToChangeset(text string) Changeset {
	n := len(text)
	if n == 0 {
		return Changeset{OldLen: 0, NewLen: 0, Ops: []Op{}}
	}
	return Changeset{
		OldLen: 0,
		NewLen: n,
		Ops:    []Op{{Type: OpInsert, Chars: text}},
	}
}

// create insert operation
func MakeInsert(oldLen, pos int, text string) Changeset {
	ops := []Op{}
	if pos > 0 {
		ops = append(ops, Op{Type: OpRetain, Len: pos})
	}
	ops = append(ops, Op{Type: OpInsert, Chars: text})
	tail := oldLen - pos
	if tail > 0 {
		ops = append(ops, Op{Type: OpRetain, Len: tail})
	}
	return Changeset{
		OldLen: oldLen,
		NewLen: oldLen + len(text),
		Ops:    ops,
	}
}

// create delete operation
func MakeDelete(oldLen, pos, count int) Changeset {
	ops := []Op{}
	if pos > 0 {
		ops = append(ops, Op{Type: OpRetain, Len: pos})
	}
	ops = append(ops, Op{Type: OpDelete, Len: count})
	tail := oldLen - pos - count
	if tail > 0 {
		ops = append(ops, Op{Type: OpRetain, Len: tail})
	}
	return Changeset{
		OldLen: oldLen,
		NewLen: oldLen - count,
		Ops:    ops,
	}
}

// validation logic
func (c Changeset) Validate() error {
	sourceConsumed := 0
	newLen := 0
	for _, op := range c.Ops {
		switch op.Type {
		case OpRetain:
			if op.Len <= 0 {
				return fmt.Errorf("retain len must be positive, got %d", op.Len)
			}
			sourceConsumed += op.Len
			newLen += op.Len
		case OpInsert:
			if len(op.Chars) == 0 {
				return fmt.Errorf("insert chars must not be empty")
			}
			newLen += len(op.Chars)
		case OpDelete:
			if op.Len <= 0 {
				return fmt.Errorf("delete len must be positive, got %d", op.Len)
			}
			sourceConsumed += op.Len
		default:
			return fmt.Errorf("unknown op type: %s", op.Type)
		}
	}
	if sourceConsumed != c.OldLen {
		return fmt.Errorf("ops consume %d chars but OldLen=%d", sourceConsumed, c.OldLen)
	}
	if newLen != c.NewLen {
		return fmt.Errorf("ops produce %d chars but NewLen=%d", newLen, c.NewLen)
	}
	return nil
}



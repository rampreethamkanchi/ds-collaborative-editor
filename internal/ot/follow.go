// follow logic
package ot

// rebase B on top of A
func Follow(A, B Changeset) (Changeset, error) {
	result := Changeset{
		OldLen: A.NewLen,
		NewLen: 0,
	}

	// helper for ops
	type opSlice struct {
		ops []Op
		pos int
		off int
	}

	peek := func(s *opSlice) (Op, bool) {
		if s.pos >= len(s.ops) {
			return Op{}, false
		}
		op := s.ops[s.pos]
		if op.Type == OpInsert {
			op.Chars = op.Chars[s.off:]
		} else {
			op.Len -= s.off
		}
		return op, true
	}

	consume := func(s *opSlice, n int) {
		for n > 0 && s.pos < len(s.ops) {
			op := s.ops[s.pos]
			var rem int
			if op.Type == OpInsert {
				rem = len(op.Chars) - s.off
			} else {
				rem = op.Len - s.off
			}
			if n >= rem {
				n -= rem
				s.pos++
				s.off = 0
			} else {
				s.off += n
				n = 0
			}
		}
	}

	// add op to result
	addOp := func(op Op) {
		if len(result.Ops) > 0 {
			last := &result.Ops[len(result.Ops)-1]
			if last.Type == op.Type {
				switch op.Type {
				case OpRetain:
					last.Len += op.Len
					result.NewLen += op.Len
					return
				case OpDelete:
					last.Len += op.Len
					return
				case OpInsert:
					last.Chars += op.Chars
					result.NewLen += len(op.Chars)
					return
				}
			}
		}
		result.Ops = append(result.Ops, op)
		switch op.Type {
		case OpRetain:
			result.NewLen += op.Len
		case OpInsert:
			result.NewLen += len(op.Chars)
		}
	}

	aSlice := &opSlice{ops: A.Ops}
	bSlice := &opSlice{ops: B.Ops}

	for {
		aOp, aOk := peek(aSlice)
		bOp, bOk := peek(bSlice)

		if !aOk && !bOk {
			break
		}

		// both inserting case
		if aOk && aOp.Type == OpInsert && bOk && bOp.Type == OpInsert {
			if aOp.Chars <= bOp.Chars {
				addOp(Op{Type: OpRetain, Len: len(aOp.Chars)})
				consume(aSlice, len(aOp.Chars))
			} else {
				addOp(Op{Type: OpInsert, Chars: bOp.Chars})
				consume(bSlice, len(bOp.Chars))
			}
			continue
		}

		// A inserts case
		if aOk && aOp.Type == OpInsert {
			addOp(Op{Type: OpRetain, Len: len(aOp.Chars)})
			consume(aSlice, len(aOp.Chars))
			continue
		}

		// B inserts case
		if bOk && bOp.Type == OpInsert {
			addOp(Op{Type: OpInsert, Chars: bOp.Chars})
			consume(bSlice, len(bOp.Chars))
			continue
		}

		// normal case
		if !aOk || !bOk {
			break
		}

		n := aOp.Len
		if bOp.Len < n {
			n = bOp.Len
		}

		switch {
		case aOp.Type == OpRetain && bOp.Type == OpRetain:
			addOp(Op{Type: OpRetain, Len: n})

		case aOp.Type == OpRetain && bOp.Type == OpDelete:
			addOp(Op{Type: OpDelete, Len: n})

		case aOp.Type == OpDelete && bOp.Type == OpRetain:
			// already deleted

		case aOp.Type == OpDelete && bOp.Type == OpDelete:
			// already deleted
		}

		consume(aSlice, n)
		consume(bSlice, n)
	}

	return result, nil
}

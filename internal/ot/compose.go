// compose logic
package ot

// merge two changesets
func Compose(A, B Changeset) (Changeset, error) {
	result := Changeset{OldLen: A.OldLen, NewLen: B.NewLen}

	// add op to list
	addOp := func(op Op) {
		if len(result.Ops) > 0 {
			last := &result.Ops[len(result.Ops)-1]
			if last.Type == op.Type {
				switch op.Type {
				case OpRetain:
					last.Len += op.Len
					return
				case OpDelete:
					last.Len += op.Len
					return
				case OpInsert:
					last.Chars += op.Chars
					return
				}
			}
		}
		result.Ops = append(result.Ops, op)
	}

	// helper for ops
	type opSlice struct {
		ops []Op
		pos int
		off int
	}

	// check next op
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

	// move forward
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

	aSlice := &opSlice{ops: A.Ops}
	bSlice := &opSlice{ops: B.Ops}

	for {
		bOp, bOk := peek(bSlice)
		aOp, aOk := peek(aSlice)

		if !bOk && !aOk {
			break
		}

		// handle insert case
		if bOk && bOp.Type == OpInsert {
			addOp(Op{Type: OpInsert, Chars: bOp.Chars})
			consume(bSlice, len(bOp.Chars))
			continue
		}

		// handle delete case
		if aOk && aOp.Type == OpDelete {
			addOp(Op{Type: OpDelete, Len: aOp.Len})
			consume(aSlice, aOp.Len)
			continue
		}

		// normal processing
		if !aOk || !bOk {
			break
		}

		var aLen int
		if aOp.Type == OpInsert {
			aLen = len(aOp.Chars)
		} else {
			aLen = aOp.Len
		}
		bLen := bOp.Len
		n := aLen
		if bLen < n {
			n = bLen
		}

		switch {
		case aOp.Type == OpRetain && bOp.Type == OpRetain:
			addOp(Op{Type: OpRetain, Len: n})

		case aOp.Type == OpRetain && bOp.Type == OpDelete:
			addOp(Op{Type: OpDelete, Len: n})

		case aOp.Type == OpInsert && bOp.Type == OpRetain:
			addOp(Op{Type: OpInsert, Chars: aOp.Chars[:n]})

		case aOp.Type == OpInsert && bOp.Type == OpDelete:
			// nothing to do
		}

		consume(aSlice, n)
		consume(bSlice, n)
	}

	return result, nil
}

// diff logic
package ot

// find difference between texts
func DiffToChangeset(oldText, newText string) Changeset {
	old := []rune(oldText)
	nw := []rune(newText)
	n, m := len(old), len(nw)

	// lcs table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if old[i-1] == nw[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	// backtrack for ops
	type seg struct {
		kind rune
		r    rune
	}
	var segs []seg
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && old[i-1] == nw[j-1]:
			segs = append(segs, seg{'=', old[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			segs = append(segs, seg{'+', nw[j-1]})
			j--
		default:
			segs = append(segs, seg{'-', old[i-1]})
			i--
		}
	}

	// reverse it
	for l, r := 0, len(segs)-1; l < r; l, r = l+1, r-1 {
		segs[l], segs[r] = segs[r], segs[l]
	}

	// combine segments
	c := Changeset{OldLen: len(oldText), NewLen: len(newText)}
	addOp := func(op Op) {
		if len(c.Ops) > 0 {
			last := &c.Ops[len(c.Ops)-1]
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
		c.Ops = append(c.Ops, op)
	}

	for _, s := range segs {
		switch s.kind {
		case '=':
			addOp(Op{Type: OpRetain, Len: len(string(s.r))})
		case '+':
			addOp(Op{Type: OpInsert, Chars: string(s.r)})
		case '-':
			addOp(Op{Type: OpDelete, Len: len(string(s.r))})
		}
	}

	return c
}

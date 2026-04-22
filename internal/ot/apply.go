// apply logic
package ot

import (
	"fmt"
	"strings"
)

// update text using changeset
func ApplyChangeset(text string, c Changeset) (string, error) {
	// length check
	if c.OldLen != len(text) {
		return "", fmt.Errorf(
			"ApplyChangeset: changeset OldLen=%d does not match text length=%d",
			c.OldLen, len(text),
		)
	}

	var result strings.Builder
	result.Grow(c.NewLen)

	srcPos := 0

	for _, op := range c.Ops {
		switch op.Type {
		case OpRetain:
			// copy chars
			if srcPos+op.Len > len(text) {
				return "", fmt.Errorf(
					"ApplyChangeset: retain %d at pos %d exceeds text length %d",
					op.Len, srcPos, len(text),
				)
			}
			result.WriteString(text[srcPos : srcPos+op.Len])
			srcPos += op.Len

		case OpInsert:
			// add chars
			result.WriteString(op.Chars)

		case OpDelete:
			// skip chars
			if srcPos+op.Len > len(text) {
				return "", fmt.Errorf(
					"ApplyChangeset: delete %d at pos %d exceeds text length %d",
					op.Len, srcPos, len(text),
				)
			}
			srcPos += op.Len

		default:
			return "", fmt.Errorf("ApplyChangeset: unknown op type: %s", op.Type)
		}
	}

	// final check
	if srcPos != len(text) {
		return "", fmt.Errorf(
			"ApplyChangeset: only consumed %d of %d source chars",
			srcPos, len(text),
		)
	}

	return result.String(), nil
}

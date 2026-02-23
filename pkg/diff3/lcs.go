package diff3

// DiffType classifies a line in an edit script.
type DiffType int

const (
	Equal  DiffType = iota // Line is unchanged between a and b.
	Insert                 // Line was inserted (present in b only).
	Delete                 // Line was deleted (present in a only).
)

// DiffOp is a single operation in an edit script produced by MyersDiff.
type DiffOp struct {
	Type DiffType
	Line string
}

// MyersDiff computes the shortest edit script to transform a into b
// using the Myers diff algorithm operating on whole lines.
//
// The algorithm runs in O((N+M)*D) time where N and M are the lengths
// of a and b, and D is the size of the minimum edit script.
func MyersDiff(a, b []string) []DiffOp {
	n := len(a)
	m := len(b)

	// Handle trivial cases.
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]DiffOp, m)
		for i, line := range b {
			ops[i] = DiffOp{Type: Insert, Line: line}
		}
		return ops
	}
	if m == 0 {
		ops := make([]DiffOp, n)
		for i, line := range a {
			ops[i] = DiffOp{Type: Delete, Line: line}
		}
		return ops
	}

	// Myers algorithm.
	max := n + m
	size := 2*max + 1

	v := make([]int, size)
	for i := range v {
		v[i] = 0
	}

	// trace[d] holds a snapshot of v after processing edit distance d.
	var trace [][]int

	for d := 0; d <= max; d++ {
		for k := -d; k <= d; k += 2 {
			idx := k + max
			var x int
			if k == -d || (k != d && v[idx-1] < v[idx+1]) {
				x = v[idx+1] // move down (insert)
			} else {
				x = v[idx-1] + 1 // move right (delete)
			}
			y := x - k

			// Follow diagonal (equal lines).
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[idx] = x

			if x >= n && y >= m {
				// Save this final state and backtrack.
				snap := make([]int, size)
				copy(snap, v)
				trace = append(trace, snap)
				return backtrack(trace, a, b, d)
			}
		}

		// Save snapshot of v after this d-step.
		snap := make([]int, size)
		copy(snap, v)
		trace = append(trace, snap)
	}

	// Should never reach here for valid inputs.
	return nil
}

// backtrack reconstructs the edit script from the trace of v snapshots.
// trace[d] holds the v-array state after processing edit distance d.
func backtrack(trace [][]int, a, b []string, dFinal int) []DiffOp {
	n := len(a)
	m := len(b)
	max := n + m

	x := n
	y := m

	// Build the edit script in reverse.
	var ops []DiffOp

	for d := dFinal; d > 0; d-- {
		k := x - y
		idx := k + max

		vPrev := trace[d-1]

		var prevK int
		if k == -d || (k != d && vPrev[idx-1] < vPrev[idx+1]) {
			prevK = k + 1 // came from an insert (down move)
		} else {
			prevK = k - 1 // came from a delete (right move)
		}

		prevX := vPrev[prevK+max]
		prevY := prevX - prevK

		// Trace back along the diagonal (equal lines).
		for x > prevX && y > prevY {
			x--
			y--
			ops = append(ops, DiffOp{Type: Equal, Line: a[x]})
		}

		if k == prevK+1 {
			// This was a delete (right move): prevK = k-1.
			x--
			ops = append(ops, DiffOp{Type: Delete, Line: a[x]})
		} else {
			// This was an insert (down move): prevK = k+1.
			y--
			ops = append(ops, DiffOp{Type: Insert, Line: b[y]})
		}
	}

	// Remaining diagonal at d=0.
	for x > 0 && y > 0 {
		x--
		y--
		ops = append(ops, DiffOp{Type: Equal, Line: a[x]})
	}

	// Reverse to get forward order.
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	return ops
}

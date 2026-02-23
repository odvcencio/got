package diff3

import (
	"bytes"
	"strings"
)

// HunkType classifies a hunk in a three-way merge result.
type HunkType int

const (
	HunkClean    HunkType = iota // Hunk was merged cleanly.
	HunkConflict                 // Hunk has a conflict that requires manual resolution.
)

// Hunk represents a contiguous section of the merge output.
type Hunk struct {
	Type                       HunkType
	Base, Ours, Theirs, Merged []byte
}

// Result holds the outcome of a three-way merge.
type Result struct {
	Merged       []byte // Full merged content (with conflict markers if conflicts exist).
	HasConflicts bool   // True if any hunk is a conflict.
	Hunks        []Hunk // Individual hunks in document order.
}

// DiffLine is a single line in the output of LineDiff.
type DiffLine struct {
	Type    DiffType
	Content string
}

// LineDiff computes a line-level diff between byte slices a and b.
// It is intended for use by the `got diff` command.
func LineDiff(a, b []byte) []DiffLine {
	aLines := splitLines(string(a))
	bLines := splitLines(string(b))

	ops := MyersDiff(aLines, bLines)

	result := make([]DiffLine, len(ops))
	for i, op := range ops {
		result[i] = DiffLine{Type: op.Type, Content: op.Line}
	}
	return result
}

// Merge performs a three-way merge of base, ours, and theirs.
//
// Algorithm:
//  1. Split base, ours, theirs into lines.
//  2. Compute diff(base, ours) and diff(base, theirs).
//  3. Convert each diff into a sequence of "chunks" — contiguous runs of
//     unchanged or changed regions relative to the base.
//  4. Walk through base lines, consulting both chunk sequences to decide
//     how each base region is handled.
//  5. When both sides change the same base region differently, emit a conflict.
func Merge(base, ours, theirs []byte) Result {
	baseLines := splitLines(string(base))
	oursLines := splitLines(string(ours))
	theirsLines := splitLines(string(theirs))

	oursChunks := buildChunks(baseLines, oursLines)
	theirsChunks := buildChunks(baseLines, theirsLines)

	return mergeChunks(baseLines, oursChunks, theirsChunks)
}

// splitLines splits s into lines. A trailing newline does not produce
// an extra empty element (matching standard text file conventions).
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty string caused by a final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// chunk represents a contiguous region relative to the base.
type chunk struct {
	baseStart, baseEnd int      // range [baseStart, baseEnd) in base
	lines              []string // replacement lines for this region
	changed            bool     // true if this region differs from base
}

// buildChunks converts a two-way diff (base → side) into a list of chunks.
// Each chunk covers a contiguous range of base lines and carries the
// corresponding replacement lines from the side.
func buildChunks(base, side []string) []chunk {
	ops := MyersDiff(base, side)

	var chunks []chunk
	baseIdx := 0

	i := 0
	for i < len(ops) {
		op := ops[i]

		if op.Type == Equal {
			// One equal line → unchanged chunk.
			chunks = append(chunks, chunk{
				baseStart: baseIdx,
				baseEnd:   baseIdx + 1,
				lines:     []string{op.Line},
				changed:   false,
			})
			baseIdx++
			i++
			continue
		}

		// Accumulate a contiguous changed region (deletes and/or inserts).
		chunkStart := baseIdx
		var sideLines []string

		for i < len(ops) && ops[i].Type != Equal {
			if ops[i].Type == Delete {
				baseIdx++
			} else { // Insert
				sideLines = append(sideLines, ops[i].Line)
			}
			i++
		}

		chunks = append(chunks, chunk{
			baseStart: chunkStart,
			baseEnd:   baseIdx,
			lines:     sideLines,
			changed:   true,
		})
	}

	return chunks
}

// mergeChunks walks two chunk sequences (ours and theirs) in parallel,
// aligned by base-line positions, to produce the merge result.
func mergeChunks(baseLines []string, oursChunks, theirsChunks []chunk) Result {
	var merged bytes.Buffer
	var hunks []Hunk
	hasConflicts := false

	oi := 0 // index into oursChunks
	ti := 0 // index into theirsChunks

	basePos := 0 // current position in base

	for oi < len(oursChunks) || ti < len(theirsChunks) {
		// Determine which chunk(s) to process next based on baseStart.
		var oc, tc *chunk
		if oi < len(oursChunks) {
			oc = &oursChunks[oi]
		}
		if ti < len(theirsChunks) {
			tc = &theirsChunks[ti]
		}

		if oc == nil {
			// Only theirs left.
			writeChunk(&merged, tc)
			hunks = append(hunks, makeCleanHunk(baseLines, tc))
			basePos = tc.baseEnd
			ti++
			continue
		}
		if tc == nil {
			// Only ours left.
			writeChunk(&merged, oc)
			hunks = append(hunks, makeCleanHunk(baseLines, oc))
			basePos = oc.baseEnd
			oi++
			continue
		}

		// Both chunks available. They should cover the same base region
		// since they are derived from the same base.
		if oc.baseStart == tc.baseStart && oc.baseEnd == tc.baseEnd {
			// Chunks are aligned.
			if !oc.changed && !tc.changed {
				// Both unchanged → take base.
				writeChunk(&merged, oc)
				hunks = append(hunks, makeCleanHunk(baseLines, oc))
			} else if oc.changed && !tc.changed {
				// Only ours changed → take ours.
				writeChunk(&merged, oc)
				hunks = append(hunks, makeCleanHunk(baseLines, oc))
			} else if !oc.changed && tc.changed {
				// Only theirs changed → take theirs.
				writeChunk(&merged, tc)
				hunks = append(hunks, makeCleanHunk(baseLines, tc))
			} else {
				// Both changed.
				if linesEqual(oc.lines, tc.lines) {
					// Identical change → take either, clean.
					writeChunk(&merged, oc)
					hunks = append(hunks, makeCleanHunk(baseLines, oc))
				} else {
					// Conflict.
					hasConflicts = true
					writeConflict(&merged, oc.lines, tc.lines)
					hunks = append(hunks, makeConflictHunk(baseLines, oc, tc))
				}
			}
			basePos = oc.baseEnd
			oi++
			ti++
			continue
		}

		// Chunks are misaligned. This happens when one side has a change
		// that spans multiple base-aligned chunks on the other side.
		// We need to collect all overlapping chunks from both sides.
		regionStart := min(oc.baseStart, tc.baseStart)
		regionEnd := max(oc.baseEnd, tc.baseEnd)

		// Gather all ours chunks that overlap [regionStart, regionEnd).
		var oursRegion []chunk
		for oi < len(oursChunks) && oursChunks[oi].baseStart < regionEnd {
			oursRegion = append(oursRegion, oursChunks[oi])
			if oursChunks[oi].baseEnd > regionEnd {
				regionEnd = oursChunks[oi].baseEnd
			}
			oi++
		}

		// Gather all theirs chunks that overlap [regionStart, regionEnd).
		var theirsRegion []chunk
		for ti < len(theirsChunks) && theirsChunks[ti].baseStart < regionEnd {
			theirsRegion = append(theirsRegion, theirsChunks[ti])
			if theirsChunks[ti].baseEnd > regionEnd {
				regionEnd = theirsChunks[ti].baseEnd
			}
			ti++
		}

		// Reassemble lines for each side over the region.
		oursOut := assembleRegion(oursRegion)
		theirsOut := assembleRegion(theirsRegion)
		anyOursChanged := anyChanged(oursRegion)
		anyTheirsChanged := anyChanged(theirsRegion)

		baseRegion := baseLines[regionStart:regionEnd]

		if !anyOursChanged && !anyTheirsChanged {
			for _, l := range baseRegion {
				merged.WriteString(l)
				merged.WriteByte('\n')
			}
			hunks = append(hunks, Hunk{
				Type:   HunkClean,
				Base:   joinLines(baseRegion),
				Merged: joinLines(baseRegion),
			})
		} else if anyOursChanged && !anyTheirsChanged {
			for _, l := range oursOut {
				merged.WriteString(l)
				merged.WriteByte('\n')
			}
			hunks = append(hunks, Hunk{
				Type:   HunkClean,
				Base:   joinLines(baseRegion),
				Ours:   joinLines(oursOut),
				Merged: joinLines(oursOut),
			})
		} else if !anyOursChanged && anyTheirsChanged {
			for _, l := range theirsOut {
				merged.WriteString(l)
				merged.WriteByte('\n')
			}
			hunks = append(hunks, Hunk{
				Type:   HunkClean,
				Base:   joinLines(baseRegion),
				Theirs: joinLines(theirsOut),
				Merged: joinLines(theirsOut),
			})
		} else {
			// Both changed in the overlapping region.
			if linesEqual(oursOut, theirsOut) {
				for _, l := range oursOut {
					merged.WriteString(l)
					merged.WriteByte('\n')
				}
				hunks = append(hunks, Hunk{
					Type:   HunkClean,
					Base:   joinLines(baseRegion),
					Ours:   joinLines(oursOut),
					Merged: joinLines(oursOut),
				})
			} else {
				hasConflicts = true
				writeConflict(&merged, oursOut, theirsOut)
				hunks = append(hunks, Hunk{
					Type:   HunkConflict,
					Base:   joinLines(baseRegion),
					Ours:   joinLines(oursOut),
					Theirs: joinLines(theirsOut),
				})
			}
		}
		basePos = regionEnd
	}

	_ = basePos // suppress unused warning

	return Result{
		Merged:       merged.Bytes(),
		HasConflicts: hasConflicts,
		Hunks:        hunks,
	}
}

func writeChunk(buf *bytes.Buffer, c *chunk) {
	for _, l := range c.lines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
}

func writeConflict(buf *bytes.Buffer, oursLines, theirsLines []string) {
	buf.WriteString("<<<<<<< ours\n")
	for _, l := range oursLines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	buf.WriteString("=======\n")
	for _, l := range theirsLines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	buf.WriteString(">>>>>>> theirs\n")
}

func makeCleanHunk(baseLines []string, c *chunk) Hunk {
	h := Hunk{
		Type:   HunkClean,
		Merged: joinLines(c.lines),
	}
	if c.baseStart < c.baseEnd {
		h.Base = joinLines(baseLines[c.baseStart:c.baseEnd])
	}
	if c.changed {
		h.Ours = joinLines(c.lines)
	}
	return h
}

func makeConflictHunk(baseLines []string, oc, tc *chunk) Hunk {
	h := Hunk{
		Type: HunkConflict,
		Ours: joinLines(oc.lines),
	}
	h.Theirs = joinLines(tc.lines)
	if oc.baseStart < oc.baseEnd {
		h.Base = joinLines(baseLines[oc.baseStart:oc.baseEnd])
	}
	return h
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assembleRegion(chunks []chunk) []string {
	var lines []string
	for _, c := range chunks {
		lines = append(lines, c.lines...)
	}
	return lines
}

func anyChanged(chunks []chunk) bool {
	for _, c := range chunks {
		if c.changed {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

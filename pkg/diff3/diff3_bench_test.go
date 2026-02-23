package diff3

import (
	"fmt"
	"strings"
	"testing"
)

// generateLines builds a slice of n numbered lines.
func generateLines(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line-%04d\n", i)
	}
	return []byte(b.String())
}

// modifyLine replaces a single line in source at the given 0-based index.
func modifyLine(src []byte, lineIdx int, replacement string) []byte {
	lines := strings.Split(string(src), "\n")
	if lineIdx < len(lines) {
		lines[lineIdx] = replacement
	}
	return []byte(strings.Join(lines, "\n"))
}

// BenchmarkDiff3Small benchmarks a three-way merge on 50-line files
// with non-overlapping single-line changes.
func BenchmarkDiff3Small(b *testing.B) {
	const n = 50
	base := generateLines(n)
	ours := modifyLine(base, 5, "OURS-CHANGED-LINE")
	theirs := modifyLine(base, 45, "THEIRS-CHANGED-LINE")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := Merge(base, ours, theirs)
		if r.HasConflicts {
			b.Fatal("unexpected conflict in small merge")
		}
	}
}

// BenchmarkDiff3Large benchmarks a three-way merge on 1000-line files
// with non-overlapping single-line changes far apart.
func BenchmarkDiff3Large(b *testing.B) {
	const n = 1000
	base := generateLines(n)
	ours := modifyLine(base, 50, "OURS-CHANGED-LINE")
	theirs := modifyLine(base, 950, "THEIRS-CHANGED-LINE")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := Merge(base, ours, theirs)
		if r.HasConflicts {
			b.Fatal("unexpected conflict in large merge")
		}
	}
}

// BenchmarkMyersDiff benchmarks the Myers diff algorithm on 500-line files
// with a single-line modification.
func BenchmarkMyersDiff(b *testing.B) {
	const n = 500
	a := make([]string, n)
	for i := 0; i < n; i++ {
		a[i] = fmt.Sprintf("line-%04d", i)
	}
	bLines := make([]string, n)
	copy(bLines, a)
	bLines[250] = "MODIFIED-LINE"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops := MyersDiff(a, bLines)
		if len(ops) == 0 {
			b.Fatal("expected non-empty diff")
		}
	}
}

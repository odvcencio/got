package merge

import (
	"bytes"
	"fmt"
	"testing"
)

// generateMergeGoSource builds a Go file with numFuncs functions.
// Each function body contains the given bodyLine so that sources
// can be differentiated for ours/theirs variants.
func generateMergeGoSource(numFuncs int, bodyFn func(i int) string) []byte {
	var buf bytes.Buffer
	buf.WriteString("package main\n\nimport \"fmt\"\n\n")
	for i := 0; i < numFuncs; i++ {
		fmt.Fprintf(&buf, "func Func%d() {\n\t%s\n}\n\n", i, bodyFn(i))
	}
	return buf.Bytes()
}

// BenchmarkMergeClean benchmarks a clean structural merge where ours
// adds functions at the end and theirs adds different functions at the end,
// with no overlapping changes to existing entities.
func BenchmarkMergeClean(b *testing.B) {
	const baseFuncs = 20
	const addedFuncs = 5

	base := generateMergeGoSource(baseFuncs, func(i int) string {
		return fmt.Sprintf("fmt.Println(%d)", i)
	})
	// Ours: same base functions + 5 new ones named OursFunc0..4
	var oursBuf bytes.Buffer
	oursBuf.Write(base)
	for i := 0; i < addedFuncs; i++ {
		fmt.Fprintf(&oursBuf, "func OursFunc%d() {\n\tfmt.Println(\"ours-%d\")\n}\n\n", i, i)
	}
	ours := oursBuf.Bytes()

	// Theirs: same base functions + 5 new ones named TheirsFunc0..4
	var theirsBuf bytes.Buffer
	theirsBuf.Write(base)
	for i := 0; i < addedFuncs; i++ {
		fmt.Fprintf(&theirsBuf, "func TheirsFunc%d() {\n\tfmt.Println(\"theirs-%d\")\n}\n\n", i, i)
	}
	theirs := theirsBuf.Bytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := MergeFiles("bench.go", base, ours, theirs)
		if err != nil {
			b.Fatalf("MergeFiles failed: %v", err)
		}
		if result.HasConflicts {
			b.Fatal("expected clean merge, got conflicts")
		}
	}
}

// BenchmarkMergeConflict benchmarks a structural merge where both sides
// modify the same function differently, producing one conflict.
func BenchmarkMergeConflict(b *testing.B) {
	const numFuncs = 20

	base := generateMergeGoSource(numFuncs, func(i int) string {
		return fmt.Sprintf("fmt.Println(%d)", i)
	})
	// Ours: modify Func0's body
	ours := generateMergeGoSource(numFuncs, func(i int) string {
		if i == 0 {
			return "fmt.Println(\"ours-modified\")"
		}
		return fmt.Sprintf("fmt.Println(%d)", i)
	})
	// Theirs: modify Func0's body differently
	theirs := generateMergeGoSource(numFuncs, func(i int) string {
		if i == 0 {
			return "fmt.Println(\"theirs-modified\")"
		}
		return fmt.Sprintf("fmt.Println(%d)", i)
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := MergeFiles("bench.go", base, ours, theirs)
		if err != nil {
			b.Fatalf("MergeFiles failed: %v", err)
		}
		if !result.HasConflicts {
			b.Fatal("expected conflicts, got clean merge")
		}
	}
}

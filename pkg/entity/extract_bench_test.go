package entity

import (
	"bytes"
	"fmt"
	"testing"
)

// generateGoSource builds a syntactically valid Go file with numFuncs
// top-level functions, suitable for benchmarking entity extraction.
func generateGoSource(numFuncs int) []byte {
	var buf bytes.Buffer
	buf.WriteString("package main\n\n")
	for i := 0; i < numFuncs; i++ {
		fmt.Fprintf(&buf, "func Func%d() {\n\tprintln(%d)\n}\n\n", i, i)
	}
	return buf.Bytes()
}

func benchmarkExtract(b *testing.B, numFuncs int) {
	b.Helper()
	src := generateGoSource(numFuncs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Extract("bench.go", src)
		if err != nil {
			b.Fatalf("Extract failed: %v", err)
		}
	}
}

func BenchmarkExtract10Functions(b *testing.B) {
	benchmarkExtract(b, 10)
}

func BenchmarkExtract50Functions(b *testing.B) {
	benchmarkExtract(b, 50)
}

func BenchmarkExtract200Functions(b *testing.B) {
	benchmarkExtract(b, 200)
}

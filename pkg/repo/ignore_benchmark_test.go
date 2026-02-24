package repo

import (
	"fmt"
	"path/filepath"
	"testing"
)

var benchmarkIgnoreSink bool

func BenchmarkIgnoreCheckerLargeLiteralSet(b *testing.B) {
	const literalPatternCount = 10000

	lines := make([]string, 0, literalPatternCount+4)
	for i := 0; i < literalPatternCount; i++ {
		lines = append(lines, fmt.Sprintf("artifact-%05d.bin", i))
	}
	lines = append(lines,
		"*.log",
		"build/",
		"!build/keep.log",
		"**/*.gen.go",
	)

	ic := newBenchmarkIgnoreChecker(lines)
	paths := []string{
		"artifact-09999.bin",
		"src/artifact-09999.bin",
		"build/out.o",
		"build/keep.log",
		"cmd/file.gen.go",
		"src/other.txt",
	}

	b.Run("compiled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkIgnoreSink = ic.IsIgnored(paths[i%len(paths)])
		}
	})

	b.Run("legacy-scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkIgnoreSink = legacyScanIsIgnored(ic.patterns, paths[i%len(paths)])
		}
	})
}

func newBenchmarkIgnoreChecker(lines []string) *IgnoreChecker {
	ic := &IgnoreChecker{}
	ic.patterns = append(ic.patterns,
		ignorePattern{pattern: ".got", dirOnly: false, hasSlash: false},
		ignorePattern{pattern: ".git", dirOnly: false, hasSlash: false},
	)

	for _, line := range lines {
		p := parseLine(line)
		if p != nil {
			ic.patterns = append(ic.patterns, *p)
		}
	}

	ic.compile()
	return ic
}

func legacyScanIsIgnored(patterns []ignorePattern, path string) bool {
	path = filepath.ToSlash(path)

	ignored := false
	for _, p := range patterns {
		if p.matches(path) {
			ignored = !p.negated
		}
	}
	return ignored
}

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

func BenchmarkIgnoreCheckerLargeWildcardSet(b *testing.B) {
	const wildcardPatternCount = 8000

	lines := make([]string, 0, wildcardPatternCount*2+3)
	for i := 0; i < wildcardPatternCount; i++ {
		lines = append(lines, fmt.Sprintf("bucket-%05d-*.tmp", i))
		lines = append(lines, fmt.Sprintf("dir-%05d/**/gen-*.go", i))
	}
	lines = append(lines,
		"**/*.snapshot",
		"!dir-07999/keep/gen-keep.go",
		"dir-07999/keep/*.go",
	)

	ic := newBenchmarkIgnoreChecker(lines)
	wildcardScan := buildWildcardScanIndex(ic.patterns)

	paths := []string{
		"src/bucket-07999-item.tmp",
		"dir-07999/pkg/nested/gen-file.go",
		"dir-07999/keep/gen-keep.go",
		"misc/file.snapshot",
		"misc/file.txt",
	}

	b.Run("prefix-bucketed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkIgnoreSink = ic.IsIgnored(paths[i%len(paths)])
		}
	})

	b.Run("wildcard-linear-scan", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkIgnoreSink = compiledLinearWildcardIsIgnored(ic, wildcardScan, paths[i%len(paths)])
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

type wildcardScanIndex struct {
	path []int
	base []int
}

func buildWildcardScanIndex(patterns []ignorePattern) wildcardScanIndex {
	var index wildcardScanIndex
	for idx, p := range patterns {
		if p.dirOnly || p.pattern == ".got" || p.pattern == ".git" {
			continue
		}
		if p.regex != nil || !isLiteralPattern(p.pattern) {
			if p.hasSlash {
				index.path = append(index.path, idx)
			} else {
				index.base = append(index.base, idx)
			}
		}
	}
	return index
}

func compiledLinearWildcardIsIgnored(ic *IgnoreChecker, wildcardScan wildcardScanIndex, path string) bool {
	path = filepath.ToSlash(path)
	base := filepath.Base(path)

	lastMatch := -1
	ignored := false
	apply := func(idx int) {
		if idx > lastMatch {
			lastMatch = idx
			ignored = !ic.patterns[idx].negated
		}
	}
	applyAll := func(patterns []int) {
		for _, idx := range patterns {
			apply(idx)
		}
	}

	if idxs, ok := ic.dirPrefixPatterns[path]; ok {
		applyAll(idxs)
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if idxs, ok := ic.dirPrefixPatterns[path[:i]]; ok {
				applyAll(idxs)
			}
		}
	}

	if idxs, ok := ic.exactPathPatterns[path]; ok {
		applyAll(idxs)
	}
	if idxs, ok := ic.exactBasePatterns[base]; ok {
		applyAll(idxs)
	}

	for _, idx := range wildcardScan.path {
		if ic.patterns[idx].match(path) {
			apply(idx)
		}
	}
	for _, idx := range wildcardScan.base {
		if ic.patterns[idx].match(base) {
			apply(idx)
		}
	}

	return ignored
}

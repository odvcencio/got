package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestPrintLineDiff_IncludesHunkHeader(t *testing.T) {
	before := []byte(strings.Join([]string{
		"line01",
		"line02",
		"line03",
		"line04",
		"line05",
		"line06",
		"line07",
		"line08",
		"line09",
	}, "\n") + "\n")
	after := []byte(strings.Join([]string{
		"line01",
		"line02",
		"line03",
		"line04",
		"line05 changed",
		"line06",
		"line07",
		"line08",
		"line09",
	}, "\n") + "\n")

	out := renderLineDiff(t, before, after)

	if !strings.Contains(out, "@@ -2,7 +2,7 @@\n") {
		t.Fatalf("diff output missing expected hunk header:\n%s", out)
	}
	if !strings.Contains(out, "-line05\n") {
		t.Fatalf("diff output missing deleted line:\n%s", out)
	}
	if !strings.Contains(out, "+line05 changed\n") {
		t.Fatalf("diff output missing inserted line:\n%s", out)
	}
}

func TestPrintLineDiff_SplitsSeparatedChangesIntoMultipleHunks(t *testing.T) {
	beforeLines := makeNumberedLines(20)
	afterLines := make([]string, len(beforeLines))
	copy(afterLines, beforeLines)
	afterLines[2] = "line03 changed"
	afterLines[17] = "line18 changed"

	before := []byte(strings.Join(beforeLines, "\n") + "\n")
	after := []byte(strings.Join(afterLines, "\n") + "\n")

	out := renderLineDiff(t, before, after)

	if strings.Count(out, "@@ -") != 2 {
		t.Fatalf("expected 2 hunk headers, got %d:\n%s", strings.Count(out, "@@ -"), out)
	}
	if !strings.Contains(out, "@@ -1,6 +1,6 @@\n") {
		t.Fatalf("diff output missing first hunk header:\n%s", out)
	}
	if !strings.Contains(out, "@@ -15,6 +15,6 @@\n") {
		t.Fatalf("diff output missing second hunk header:\n%s", out)
	}
}

func TestPrintLineDiff_EmptySideRangesUseZeroStart(t *testing.T) {
	addOut := renderLineDiff(t, nil, []byte("a\nb\n"))
	if !strings.Contains(addOut, "@@ -0,0 +1,2 @@\n") {
		t.Fatalf("add diff missing zero-range hunk header:\n%s", addOut)
	}

	deleteOut := renderLineDiff(t, []byte("a\nb\n"), nil)
	if !strings.Contains(deleteOut, "@@ -1,2 +0,0 @@\n") {
		t.Fatalf("delete diff missing zero-range hunk header:\n%s", deleteOut)
	}
}

func renderLineDiff(t *testing.T, before, after []byte) string {
	t.Helper()

	var out bytes.Buffer
	if err := printLineDiff(&out, "main.go", before, after); err != nil {
		t.Fatalf("printLineDiff: %v", err)
	}
	return out.String()
}

func makeNumberedLines(n int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf("line%02d", i+1)
	}
	return lines
}

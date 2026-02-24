package merge

import (
	"strings"
	"testing"
)

func TestMergeImportsUnion(t *testing.T) {
	base := `import "fmt"`
	ours := "import (\n\t\"fmt\"\n\t\"os\"\n)"
	theirs := "import (\n\t\"fmt\"\n\t\"net/http\"\n)"

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict for independent import additions")
	}

	// Should contain all three: fmt, os, net/http
	s := string(merged)
	for _, imp := range []string{"fmt", "os", "net/http"} {
		if !strings.Contains(s, imp) {
			t.Errorf("merged output missing import %q", imp)
		}
	}
}

func TestMergeImportsBothRemove(t *testing.T) {
	base := "import (\n\t\"fmt\"\n\t\"os\"\n)"
	ours := `import "fmt"`
	theirs := `import "fmt"`

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "go")
	if conflict {
		t.Fatal("expected no conflict when both remove same import")
	}

	s := string(merged)
	if strings.Contains(s, "os") {
		t.Error("merged output should not contain removed import 'os'")
	}
	if !strings.Contains(s, "fmt") {
		t.Error("merged output should contain 'fmt'")
	}
}

func TestMergeImportsNonGoUsesDiff3Fallback(t *testing.T) {
	base := "from os.path import join\nfrom os import getenv\n"
	ours := "from os.path import abspath\nfrom os import getenv\n"
	theirs := "from os.path import join\nfrom os import listdir\n"

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "python")
	if conflict {
		t.Fatalf("expected clean merge for non-overlapping python import additions, got conflict")
	}

	s := string(merged)
	if !strings.Contains(s, "from os.path import abspath") {
		t.Fatalf("expected merged output to preserve python import syntax, got %q", s)
	}
	if !strings.Contains(s, "from os import listdir") {
		t.Fatalf("expected merged output to include theirs import, got %q", s)
	}
	if strings.Contains(s, "import from ") {
		t.Fatalf("unexpected synthetic import prefix in merged output: %q", s)
	}
}

func TestMergeRustImportsMergesDistinctPaths(t *testing.T) {
	base := "use std::io;\n"
	ours := "use std::fs;\n"
	theirs := "use std::net;\n"

	merged, conflict := MergeImports([]byte(base), []byte(ours), []byte(theirs), "rust")
	if conflict {
		t.Fatal("expected rust import merge to union distinct paths without conflict")
	}
	s := string(merged)
	if !strings.Contains(s, "use std::fs;") {
		t.Fatalf("expected merged rust imports to include ours path, got %q", s)
	}
	if !strings.Contains(s, "use std::net;") {
		t.Fatalf("expected merged rust imports to include theirs path, got %q", s)
	}
}

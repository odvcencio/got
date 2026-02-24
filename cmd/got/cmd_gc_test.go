package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/repo"
)

func TestGcCmdPacksLooseObjectsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeGcCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var first bytes.Buffer
	gcCmd := newGcCmd()
	gcCmd.SetOut(&first)
	gcCmd.SetErr(&first)
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("first gc Execute: %v\noutput:\n%s", err, first.String())
	}
	if !strings.Contains(first.String(), "packed ") {
		t.Fatalf("first gc output = %q, want to contain %q", first.String(), "packed ")
	}

	var second bytes.Buffer
	gcCmd = newGcCmd()
	gcCmd.SetOut(&second)
	gcCmd.SetErr(&second)
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("second gc Execute: %v\noutput:\n%s", err, second.String())
	}
	if !strings.Contains(second.String(), "nothing to pack") {
		t.Fatalf("second gc output = %q, want to contain %q", second.String(), "nothing to pack")
	}

	packDir := filepath.Join(dir, ".got", "objects", "pack")
	packEntries, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatalf("ReadDir(pack): %v", err)
	}

	hasPack := false
	hasIdx := false
	for _, entry := range packEntries {
		if strings.HasSuffix(entry.Name(), ".pack") {
			hasPack = true
		}
		if strings.HasSuffix(entry.Name(), ".idx") {
			hasIdx = true
		}
	}
	if !hasPack || !hasIdx {
		t.Fatalf("expected both .pack and .idx files in %s", packDir)
	}
}

func writeGcCmdFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/repo"
)

func TestVerifyCmdVerifiesPackedObjectsWhenLooseMissing(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gcSummary, err := r.Store.GC()
	if err != nil {
		t.Fatalf("Store.GC: %v", err)
	}
	if gcSummary.PackedObjects == 0 {
		t.Fatalf("Store.GC packed 0 objects, want > 0")
	}
	if gcSummary.PrunedObjects == 0 {
		t.Fatalf("Store.GC pruned 0 objects, want > 0")
	}

	if err := os.Remove(hashPathInRepoObjects(r.GotDir, commitHash)); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove(commit loose object): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var output bytes.Buffer
	verifyCmd := newVerifyCmd()
	verifyCmd.SetOut(&output)
	verifyCmd.SetErr(&output)
	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("verify Execute: %v\noutput:\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "ok: verified ") {
		t.Fatalf("verify output = %q, want to contain %q", output.String(), "ok: verified ")
	}
}

func TestVerifyCmdFailsOnCorruptPack(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gcSummary, err := r.Store.GC()
	if err != nil {
		t.Fatalf("Store.GC: %v", err)
	}
	packPath := filepath.Join(r.GotDir, "objects", "pack", gcSummary.PackFile)
	packData, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("ReadFile(pack): %v", err)
	}
	packData[len(packData)-1] ^= 0xff
	if err := os.WriteFile(packPath, packData, 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt pack): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var output bytes.Buffer
	verifyCmd := newVerifyCmd()
	verifyCmd.SetOut(&output)
	verifyCmd.SetErr(&output)
	err = verifyCmd.Execute()
	if err == nil {
		t.Fatal("verify command should fail for corrupt pack")
	}
	if !strings.Contains(err.Error(), "verify pack") {
		t.Fatalf("verify error = %q, want to contain %q", err.Error(), "verify pack")
	}
}

func TestVerifyCmdFailsOnCorruptPackIndex(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gcSummary, err := r.Store.GC()
	if err != nil {
		t.Fatalf("Store.GC: %v", err)
	}
	idxPath := filepath.Join(r.GotDir, "objects", "pack", gcSummary.IndexFile)
	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("ReadFile(index): %v", err)
	}
	idxData[len(idxData)-1] ^= 0xff
	if err := os.WriteFile(idxPath, idxData, 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt index): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var output bytes.Buffer
	verifyCmd := newVerifyCmd()
	verifyCmd.SetOut(&output)
	verifyCmd.SetErr(&output)
	err = verifyCmd.Execute()
	if err == nil {
		t.Fatal("verify command should fail for corrupt pack index")
	}
	if !strings.Contains(err.Error(), "verify pack index") {
		t.Fatalf("verify error = %q, want to contain %q", err.Error(), "verify pack index")
	}
}

func writeVerifyCmdFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func hashPathInRepoObjects(gotDir string, h object.Hash) string {
	return filepath.Join(gotDir, "objects", string(h[:2]), string(h[2:]))
}

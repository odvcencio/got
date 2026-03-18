package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
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

	if err := os.Remove(hashPathInRepoObjects(r.GraftDir, commitHash)); err != nil && !os.IsNotExist(err) {
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
	packPath := filepath.Join(r.GraftDir, "objects", "pack", gcSummary.PackFile)
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
	idxPath := filepath.Join(r.GraftDir, "objects", "pack", gcSummary.IndexFile)
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

// TestVerifyCmd_JSON_Integrity tests --json on default integrity mode.
func TestVerifyCmd_JSON_Integrity(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	// Should have counts, not results.
	if len(result.Results) != 0 {
		t.Errorf("len(results) = %d, want 0 for integrity mode", len(result.Results))
	}
	if result.LooseObjects == 0 {
		t.Error("looseObjects = 0, want > 0")
	}
}

// TestVerifyCmd_JSON_Integrity_WithPacks tests --json integrity output after GC packs objects.
func TestVerifyCmd_JSON_Integrity_WithPacks(t *testing.T) {
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

	if _, err := r.Store.GC(); err != nil {
		t.Fatalf("Store.GC: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.PackFiles == 0 {
		t.Error("packFiles = 0, want > 0 after GC")
	}
	if result.PackObjects == 0 {
		t.Error("packObjects = 0, want > 0 after GC")
	}
}

// TestVerifyCmd_JSON_Signatures tests --json --signatures outputs signature results.
func TestVerifyCmd_JSON_Signatures(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--signatures", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(result.Results))
	}
	if result.Results[0].CommitHash != string(commitHash) {
		t.Errorf("commitHash = %q, want %q", result.Results[0].CommitHash, commitHash)
	}
	// Unsigned commit (no signing key configured).
	if !result.Results[0].Unsigned {
		t.Error("unsigned = false, want true for unsigned commit")
	}
}

// TestVerifyCommitCmd_JSON tests --json on the verify commit subcommand.
func TestVerifyCommitCmd_JSON(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"commit", string(commitHash), "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(result.Results))
	}
	if result.Results[0].CommitHash != string(commitHash) {
		t.Errorf("commitHash = %q, want %q", result.Results[0].CommitHash, commitHash)
	}
	if !result.Results[0].Unsigned {
		t.Error("unsigned = false, want true for unsigned commit")
	}
}

// TestVerifyCmd_JSON_NoHumanOutput verifies --json suppresses human-readable output.
func TestVerifyCmd_JSON_NoHumanOutput(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	// Should not contain human-readable markers.
	if strings.Contains(raw, "ok: verified") {
		t.Errorf("output contains human-readable text: %s", raw)
	}
	// Should start with { (JSON object).
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Errorf("output does not start with '{': %s", raw)
	}
}

func TestVerifyPushLimitsCmdJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	large := bytes.Repeat([]byte("b"), pushObjectByteLimit+1)
	writeVerifyCmdFile(t, filepath.Join(dir, "large.bin"), large)
	if err := r.Add([]string{"large.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("large", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newVerifyCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"push-limits", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONVerifyPushLimitsOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.OK {
		t.Fatal("ok = true, want false for oversized push set")
	}
	if result.ObjectsExamined == 0 {
		t.Fatal("objectsExamined = 0, want > 0")
	}
	if len(result.Blockers) != 1 {
		t.Fatalf("len(blockers) = %d, want 1", len(result.Blockers))
	}
	if result.Blockers[0].SizeBytes != pushObjectByteLimit+1 {
		t.Fatalf("blocker size = %d, want %d", result.Blockers[0].SizeBytes, pushObjectByteLimit+1)
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

func hashPathInRepoObjects(graftDir string, h object.Hash) string {
	return filepath.Join(graftDir, "objects", string(h[:2]), string(h[2:]))
}

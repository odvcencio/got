package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestStatusCmd_Short(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), []byte("one\n"))
	if err := r.Add([]string{"tracked.txt"}); err != nil {
		t.Fatalf("Add tracked.txt: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), []byte("two\n"))
	writeTestFile(t, filepath.Join(dir, "staged.txt"), []byte("staged\n"))
	writeTestFile(t, filepath.Join(dir, "untracked.txt"), []byte("untracked\n"))
	if err := r.Add([]string{"staged.txt"}); err != nil {
		t.Fatalf("Add staged.txt: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--short"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := strings.TrimSpace(out.String())
	lines := strings.Split(raw, "\n")
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3\nraw:\n%s", len(lines), raw)
	}
	want := []string{
		"A  staged.txt",
		" M tracked.txt",
		"?? untracked.txt",
	}
	for _, line := range want {
		if !strings.Contains(raw, line) {
			t.Errorf("short status missing %q\nraw:\n%s", line, raw)
		}
	}
	if strings.Contains(raw, "on main") {
		t.Errorf("short output should not include branch header:\n%s", raw)
	}
}

func TestStatusCmd_ShortCleanState(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "clean.txt"), []byte("clean\n"))
	if err := r.Add([]string{"clean.txt"}); err != nil {
		t.Fatalf("Add clean.txt: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"-s"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("clean short status should be empty, got:\n%s", out.String())
	}
}

func TestStatusCmd_ShortConflictsWithJSON(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--short"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--json and --short cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

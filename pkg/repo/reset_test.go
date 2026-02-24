package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetUnstagesToHead(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add initial file: %v", err)
	}
	if _, err := r.Commit("alice", "initial"); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add modified file: %v", err)
	}

	before, err := r.Status()
	if err != nil {
		t.Fatalf("status before reset: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected non-empty status before reset")
	}

	if err := r.Reset([]string{"main.go"}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	after, err := r.Status()
	if err != nil {
		t.Fatalf("status after reset: %v", err)
	}
	entry := findStatusEntry(after, "main.go")
	if entry == nil {
		t.Fatalf("expected status entry for main.go after reset, got %+v", after)
	}
	if entry.IndexStatus != StatusClean {
		t.Fatalf("IndexStatus = %v, want %v", entry.IndexStatus, StatusClean)
	}
	if entry.WorkStatus != StatusDirty {
		t.Fatalf("WorkStatus = %v, want %v", entry.WorkStatus, StatusDirty)
	}
}

func TestResetRemovesStagedNewFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "new.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("add new file: %v", err)
	}

	if err := r.Reset([]string{"new.txt"}); err != nil {
		t.Fatalf("reset new file: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if _, ok := stg.Entries["new.txt"]; ok {
		t.Fatalf("expected new.txt to be unstaged, got staging entry %+v", stg.Entries["new.txt"])
	}
}

func findStatusEntry(entries []StatusEntry, path string) *StatusEntry {
	for i := range entries {
		if entries[i].Path == path {
			return &entries[i]
		}
	}
	return nil
}

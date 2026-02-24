package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestUpdateRef_WritesReflog(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h1 := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	h2 := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	if err := r.UpdateRef("refs/heads/main", h1); err != nil {
		t.Fatalf("UpdateRef(h1): %v", err)
	}
	if err := r.UpdateRef("refs/heads/main", h2); err != nil {
		t.Fatalf("UpdateRef(h2): %v", err)
	}

	entries, err := r.ReadReflog("main", 10)
	if err != nil {
		t.Fatalf("ReadReflog: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 reflog entries, got %d", len(entries))
	}
	if entries[0].NewHash != h2 {
		t.Fatalf("latest reflog new hash = %q, want %q", entries[0].NewHash, h2)
	}
	if entries[1].NewHash != h1 {
		t.Fatalf("previous reflog new hash = %q, want %q", entries[1].NewHash, h1)
	}

	assertFile(t, filepath.Join(r.GotDir, "logs", "refs", "heads", "main"))
}

func TestReadReflog_RespectsLimit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for i := 0; i < 5; i++ {
		h := object.Hash(fmt.Sprintf("%064x", i+1))
		if err := r.UpdateRef("refs/heads/main", h); err != nil {
			t.Fatalf("UpdateRef(%d): %v", i, err)
		}
	}

	entries, err := r.ReadReflog("main", 2)
	if err != nil {
		t.Fatalf("ReadReflog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
}

func TestUpdateRef_ReflogFailureIsReturned(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	logDir := filepath.Join(r.GotDir, "logs", "refs", "heads")
	if err := os.Remove(logDir); err != nil {
		t.Fatalf("remove reflog dir: %v", err)
	}
	if err := os.WriteFile(logDir, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("create reflog path blocker: %v", err)
	}

	h := object.Hash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	err = r.UpdateRef("refs/heads/main", h)
	if err == nil {
		t.Fatal("UpdateRef should fail when reflog append fails, got nil")
	}
	if !strings.Contains(err.Error(), "append reflog") {
		t.Fatalf("UpdateRef error = %q, want append reflog context", err)
	}

	got, resolveErr := r.ResolveRef("refs/heads/main")
	if resolveErr != nil {
		t.Fatalf("ResolveRef(main): %v", resolveErr)
	}
	if got != h {
		t.Fatalf("ResolveRef(main) = %q, want %q", got, h)
	}
}

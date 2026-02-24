package repo

import (
	"fmt"
	"path/filepath"
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

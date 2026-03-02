package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortlog_GroupsByAuthor(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "f.txt"), []byte("a\n"))
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("first", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("second", "bob"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("third", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	entries, err := r.Shortlog(ShortlogOptions{})
	if err != nil {
		t.Fatalf("Shortlog: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	// Default sort is by author name: alice, bob.
	if entries[0].Author != "alice" {
		t.Fatalf("entries[0].Author = %q, want %q", entries[0].Author, "alice")
	}
	if entries[0].Count != 2 {
		t.Fatalf("entries[0].Count = %d, want 2", entries[0].Count)
	}
	if entries[1].Author != "bob" {
		t.Fatalf("entries[1].Author = %q, want %q", entries[1].Author, "bob")
	}
	if entries[1].Count != 1 {
		t.Fatalf("entries[1].Count = %d, want 1", entries[1].Count)
	}
}

func TestShortlog_SortByCount(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// bob: 1 commit, alice: 2 commits, carol: 3 commits
	for i, tc := range []struct {
		content string
		author  string
		msg     string
	}{
		{"a\n", "bob", "bob-1"},
		{"b\n", "alice", "alice-1"},
		{"c\n", "carol", "carol-1"},
		{"d\n", "alice", "alice-2"},
		{"e\n", "carol", "carol-2"},
		{"f\n", "carol", "carol-3"},
	} {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(tc.content), 0o644); err != nil {
			t.Fatalf("WriteFile %d: %v", i, err)
		}
		if err := r.Add([]string{"f.txt"}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
		if _, err := r.Commit(tc.msg, tc.author); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	entries, err := r.Shortlog(ShortlogOptions{Numbered: true})
	if err != nil {
		t.Fatalf("Shortlog: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	// Sorted by count descending: carol(3), alice(2), bob(1).
	if entries[0].Author != "carol" || entries[0].Count != 3 {
		t.Fatalf("entries[0] = {%s, %d}, want {carol, 3}", entries[0].Author, entries[0].Count)
	}
	if entries[1].Author != "alice" || entries[1].Count != 2 {
		t.Fatalf("entries[1] = {%s, %d}, want {alice, 2}", entries[1].Author, entries[1].Count)
	}
	if entries[2].Author != "bob" || entries[2].Count != 1 {
		t.Fatalf("entries[2] = {%s, %d}, want {bob, 1}", entries[2].Author, entries[2].Count)
	}
}

func TestShortlog_Summary(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "f.txt"), []byte("a\n"))
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("msg1", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("msg2", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	entries, err := r.Shortlog(ShortlogOptions{Summary: true})
	if err != nil {
		t.Fatalf("Shortlog: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Author != "alice" {
		t.Fatalf("Author = %q, want %q", entries[0].Author, "alice")
	}
	if entries[0].Count != 2 {
		t.Fatalf("Count = %d, want 2", entries[0].Count)
	}
	// Summary mode still populates Titles (the CLI decides what to show).
	if len(entries[0].Titles) != 2 {
		t.Fatalf("Titles length = %d, want 2", len(entries[0].Titles))
	}
}

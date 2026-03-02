package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// TestReflog_EntityTracking creates a repo, adds a Go file with functions,
// commits, modifies the file (changing a function body), commits again, then
// reads the reflog with entities to verify entity changes.
func TestReflog_EntityTracking(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// First commit: a Go file with two functions.
	src1 := "package main\n\nfunc Hello() string { return \"hello\" }\n\nfunc World() string { return \"world\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src1), 0o644); err != nil {
		t.Fatalf("write main.go v1: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v1): %v", err)
	}
	_, err = r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit(initial): %v", err)
	}

	// Second commit: modify one function body.
	src2 := "package main\n\nfunc Hello() string { return \"hi\" }\n\nfunc World() string { return \"world\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644); err != nil {
		t.Fatalf("write main.go v2: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v2): %v", err)
	}
	_, err = r.Commit("modify Hello", "tester")
	if err != nil {
		t.Fatalf("Commit(modify Hello): %v", err)
	}

	// Read reflog with entities.
	entries, err := r.ReadReflogWithEntities("main", 10)
	if err != nil {
		t.Fatalf("ReadReflogWithEntities: %v", err)
	}

	if len(entries) < 2 {
		t.Fatalf("expected at least 2 reflog entries, got %d", len(entries))
	}

	// The newest entry (index 0) should be the second commit with entity changes.
	latest := entries[0]
	if latest.Reason != "update" {
		t.Errorf("latest reason = %q, want %q", latest.Reason, "update")
	}

	// Check that there are entity changes recorded.
	if len(latest.Entities) == 0 {
		t.Fatalf("expected entity changes in latest reflog entry, got none")
	}

	// Find a "modify" change for the Hello function.
	foundModify := false
	for _, ec := range latest.Entities {
		if ec.ChangeType == "modify" && ec.Path == "main.go" && strings.Contains(ec.EntityKey, "Hello") {
			foundModify = true
			break
		}
	}
	if !foundModify {
		t.Errorf("expected a 'modify' entity change for Hello in main.go, got: %+v", latest.Entities)
	}

	// The initial commit (index 1) should show "create" entries.
	initial := entries[1]
	if len(initial.Entities) == 0 {
		t.Fatalf("expected entity changes in initial reflog entry, got none")
	}
	createCount := 0
	for _, ec := range initial.Entities {
		if ec.ChangeType == "create" {
			createCount++
		}
	}
	if createCount == 0 {
		t.Errorf("expected 'create' entity changes in initial commit, got: %+v", initial.Entities)
	}
}

// TestReflog_ReadWithEntities writes a reflog line with entity data manually,
// reads it back, and verifies parsing.
func TestReflog_ReadWithEntities(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h1 := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	h2 := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Write an entity-enriched reflog entry manually.
	entities := []EntityChange{
		{Path: "main.go", EntityKey: "declaration:Hello", ChangeType: "modify"},
		{Path: "util.go", EntityKey: "declaration:Parse", ChangeType: "create"},
		{Path: "old.go", EntityKey: "declaration:Legacy", ChangeType: "delete"},
	}
	if err := r.appendReflogWithEntities("refs/heads/main", h1, h2, "commit", entities); err != nil {
		t.Fatalf("appendReflogWithEntities: %v", err)
	}

	// Read back with entities.
	entries, err := r.ReadReflogWithEntities("main", 10)
	if err != nil {
		t.Fatalf("ReadReflogWithEntities: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.OldHash != h1 {
		t.Errorf("OldHash = %q, want %q", entry.OldHash, h1)
	}
	if entry.NewHash != h2 {
		t.Errorf("NewHash = %q, want %q", entry.NewHash, h2)
	}
	if entry.Reason != "commit" {
		t.Errorf("Reason = %q, want %q", entry.Reason, "commit")
	}

	if len(entry.Entities) != 3 {
		t.Fatalf("expected 3 entity changes, got %d: %+v", len(entry.Entities), entry.Entities)
	}

	// Verify each entity change.
	want := []EntityChange{
		{Path: "main.go", EntityKey: "declaration:Hello", ChangeType: "modify"},
		{Path: "util.go", EntityKey: "declaration:Parse", ChangeType: "create"},
		{Path: "old.go", EntityKey: "declaration:Legacy", ChangeType: "delete"},
	}
	for i, ec := range entry.Entities {
		if ec.Path != want[i].Path {
			t.Errorf("entity[%d].Path = %q, want %q", i, ec.Path, want[i].Path)
		}
		if ec.EntityKey != want[i].EntityKey {
			t.Errorf("entity[%d].EntityKey = %q, want %q", i, ec.EntityKey, want[i].EntityKey)
		}
		if ec.ChangeType != want[i].ChangeType {
			t.Errorf("entity[%d].ChangeType = %q, want %q", i, ec.ChangeType, want[i].ChangeType)
		}
	}
}

// TestReflog_NoEntitiesGraceful writes a normal reflog line (no entity data),
// reads with ReadReflogWithEntities, and verifies Entities is empty without panics.
func TestReflog_NoEntitiesGraceful(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h1 := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	h2 := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Write a normal reflog line without entity data.
	if err := r.appendReflog("refs/heads/main", h1, h2, "update"); err != nil {
		t.Fatalf("appendReflog: %v", err)
	}

	// Read with entity-aware reader.
	entries, err := r.ReadReflogWithEntities("main", 10)
	if err != nil {
		t.Fatalf("ReadReflogWithEntities: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.OldHash != h1 {
		t.Errorf("OldHash = %q, want %q", entry.OldHash, h1)
	}
	if entry.NewHash != h2 {
		t.Errorf("NewHash = %q, want %q", entry.NewHash, h2)
	}
	if entry.Reason != "update" {
		t.Errorf("Reason = %q, want %q", entry.Reason, "update")
	}

	// Entities should be nil/empty, not cause a panic.
	if len(entry.Entities) != 0 {
		t.Errorf("expected 0 entity changes for normal reflog entry, got %d: %+v",
			len(entry.Entities), entry.Entities)
	}
}

// TestReflog_MixedEntries writes both normal and entity-enriched reflog lines,
// reads them all back, and verifies proper parsing of each.
func TestReflog_MixedEntries(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h1 := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	h2 := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	h3 := object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	// Write a normal line.
	if err := r.appendReflog("refs/heads/main", "", h1, "init"); err != nil {
		t.Fatalf("appendReflog: %v", err)
	}

	// Write an entity-enriched line.
	entities := []EntityChange{
		{Path: "main.go", EntityKey: "declaration:Foo", ChangeType: "create"},
	}
	if err := r.appendReflogWithEntities("refs/heads/main", h1, h2, "commit", entities); err != nil {
		t.Fatalf("appendReflogWithEntities: %v", err)
	}

	// Write another normal line.
	if err := r.appendReflog("refs/heads/main", h2, h3, "merge"); err != nil {
		t.Fatalf("appendReflog(merge): %v", err)
	}

	entries, err := r.ReadReflogWithEntities("main", 10)
	if err != nil {
		t.Fatalf("ReadReflogWithEntities: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Newest first: merge, commit, init.
	if entries[0].Reason != "merge" {
		t.Errorf("entries[0].Reason = %q, want %q", entries[0].Reason, "merge")
	}
	if len(entries[0].Entities) != 0 {
		t.Errorf("entries[0] should have no entities, got %d", len(entries[0].Entities))
	}

	if entries[1].Reason != "commit" {
		t.Errorf("entries[1].Reason = %q, want %q", entries[1].Reason, "commit")
	}
	if len(entries[1].Entities) != 1 {
		t.Errorf("entries[1] should have 1 entity, got %d", len(entries[1].Entities))
	}

	if entries[2].Reason != "init" {
		t.Errorf("entries[2].Reason = %q, want %q", entries[2].Reason, "init")
	}
	if len(entries[2].Entities) != 0 {
		t.Errorf("entries[2] should have no entities, got %d", len(entries[2].Entities))
	}
}

// TestDiffTreeEntities_BasicDiff verifies diffTreeEntities correctly identifies
// entity creates, modifies, and deletes between two commits.
func TestDiffTreeEntities_BasicDiff(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// First commit: a Go file with two functions.
	src1 := "package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 2 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src1), 0o644); err != nil {
		t.Fatalf("write main.go v1: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v1): %v", err)
	}
	h1, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit(initial): %v", err)
	}

	// Second commit: modify Alpha, remove Beta, add Gamma.
	src2 := "package main\n\nfunc Alpha() int { return 99 }\n\nfunc Gamma() int { return 3 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644); err != nil {
		t.Fatalf("write main.go v2: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v2): %v", err)
	}
	h2, err := r.Commit("modify", "tester")
	if err != nil {
		t.Fatalf("Commit(modify): %v", err)
	}

	changes, err := diffTreeEntities(r, h1, h2)
	if err != nil {
		t.Fatalf("diffTreeEntities: %v", err)
	}

	// Build a map for easy lookup.
	changeMap := make(map[string]string) // key -> changeType
	for _, c := range changes {
		changeMap[c.Path+":"+c.EntityKey] = c.ChangeType
	}

	// Alpha should be modified (body changed).
	if ct, ok := changeMap["main.go:declaration:Alpha"]; !ok || ct != "modify" {
		t.Errorf("expected Alpha to be 'modify', got %q (found=%v)", ct, ok)
	}

	// Beta should be deleted.
	if ct, ok := changeMap["main.go:declaration:Beta"]; !ok || ct != "delete" {
		t.Errorf("expected Beta to be 'delete', got %q (found=%v)", ct, ok)
	}

	// Gamma should be created.
	if ct, ok := changeMap["main.go:declaration:Gamma"]; !ok || ct != "create" {
		t.Errorf("expected Gamma to be 'create', got %q (found=%v)", ct, ok)
	}
}

// TestDiffTreeEntities_InitialCommit verifies that diffTreeEntities handles
// initial commit (zero old hash) by treating all entities as creates.
func TestDiffTreeEntities_InitialCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := "package main\n\nfunc Hello() string { return \"hello\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	changes, err := diffTreeEntities(r, object.Hash(zeroHash), h)
	if err != nil {
		t.Fatalf("diffTreeEntities: %v", err)
	}

	if len(changes) == 0 {
		t.Fatal("expected entity changes for initial commit, got none")
	}

	for _, c := range changes {
		if c.ChangeType != "create" {
			t.Errorf("expected all changes to be 'create' for initial commit, got %q for %s", c.ChangeType, c.EntityKey)
		}
	}
}

// TestParseEntityChanges verifies the parseEntityChanges helper.
func TestParseEntityChanges(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"main.go:declaration:Hello:create", 1},
		{"main.go:declaration:Hello:modify,util.go:declaration:Parse:create", 2},
		{"  ", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			got := parseEntityChanges(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseEntityChanges(%q) returned %d changes, want %d: %+v", tt.input, len(got), tt.want, got)
			}
		})
	}
}

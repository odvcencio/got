package repo

import (
	"path/filepath"
	"testing"
)

const historyGoSourceV1 = `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func Goodbye(name string) string {
	return fmt.Sprintf("goodbye %s", name)
}
`

const historyGoSourceV2 = `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hi %s", name)
}

func Farewell(name string) string {
	return fmt.Sprintf("farewell %s", name)
}
`

const historyGoSourceV3 = `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hi %s", name)
}

func Farewell(name string) string {
	return fmt.Sprintf("farewell %s", name)
}

func Added() error {
	return nil
}
`

func TestHistoryGrep_FindsMatchAcrossCommits(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Commit 1: v1 source with Hello and Goodbye.
	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	if _, err := r.Commit("initial commit", "alice"); err != nil {
		t.Fatalf("Commit v1: %v", err)
	}

	// Commit 2: v2 source — Goodbye renamed to Farewell.
	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV2))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	if _, err := r.Commit("rename goodbye to farewell", "bob"); err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	// Commit 3: v3 source — adds an error-returning function.
	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV3))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v3: %v", err)
	}
	if _, err := r.Commit("add error function", "carol"); err != nil {
		t.Fatalf("Commit v3: %v", err)
	}

	// Search for Goodbye — only exists in commit 1.
	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func Goodbye($$$PARAMS) string`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (Goodbye only in commit 1)", len(results))
	}
	if results[0].CommitMsg != "initial commit" {
		t.Errorf("CommitMsg = %q, want %q", results[0].CommitMsg, "initial commit")
	}
	if results[0].Path != "main.go" {
		t.Errorf("Path = %q, want %q", results[0].Path, "main.go")
	}
}

func TestHistoryGrep_FindsMatchInMultipleCommits(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit v1: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV2))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	if _, err := r.Commit("update", "bob"); err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	// Hello exists in both commits — should find matches in both.
	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func Hello($$$PARAMS) string`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (Hello in both commits)", len(results))
	}
}

func TestHistoryGrep_MaxCommitsLimitsResults(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create 3 commits, each with Hello.
	for i, src := range []string{historyGoSourceV1, historyGoSourceV2, historyGoSourceV3} {
		writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
		if err := r.Add([]string{"main.go"}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
		if _, err := r.Commit("commit", "alice"); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	// All 3 commits have Hello; limit to 1 commit.
	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func Hello($$$PARAMS) string`,
		MaxCommits: 1,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	// Only the most recent commit should be searched.
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (max-commits=1)", len(results))
	}
}

func TestHistoryGrep_CommitHashAndMessagePopulated(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("the initial commit", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func Hello($$$PARAMS) string`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if results[0].CommitHash != string(commitHash) {
		t.Errorf("CommitHash = %q, want %q", results[0].CommitHash, commitHash)
	}
	if results[0].CommitMsg != "the initial commit" {
		t.Errorf("CommitMsg = %q, want %q", results[0].CommitMsg, "the initial commit")
	}
}

func TestHistoryGrep_CapturesPopulated(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func $NAME($$$PARAMS) string`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("got %d results, want >= 2", len(results))
	}

	names := make(map[string]bool)
	for _, res := range results {
		if name, ok := res.Captures["NAME"]; ok {
			names[name] = true
		}
	}
	if !names["Hello"] {
		t.Errorf("expected capture NAME=Hello, got captures: %v", names)
	}
	if !names["Goodbye"] {
		t.Errorf("expected capture NAME=Goodbye, got captures: %v", names)
	}
}

func TestHistoryGrep_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.HistoryGrep(HistoryGrepOptions{Pattern: ""})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestHistoryGrep_PathFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	writeFile(t, filepath.Join(dir, "other.go"), []byte(`package main

func Other(s string) string {
	return s
}
`))
	if err := r.Add([]string{"main.go", "other.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Search with path filter for only main.go.
	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:     `func $NAME($$$PARAMS) string`,
		PathPattern: "main.go",
		MaxCommits:  100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	for _, res := range results {
		if res.Path != "main.go" {
			t.Errorf("path filter leaked: got result in %q, want only main.go", res.Path)
		}
	}
}

func TestHistoryGrep_NoMatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func NonExistent() error`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestHistoryGrep_EntityContext(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(historyGoSourceV1))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.HistoryGrep(HistoryGrepOptions{
		Pattern:    `func $NAME($$$PARAMS) string`,
		MaxCommits: 100,
	})
	if err != nil {
		t.Fatalf("HistoryGrep: %v", err)
	}

	if len(results) < 1 {
		t.Fatalf("got 0 results, want >= 1")
	}

	found := false
	for _, res := range results {
		if res.EntityName != "" {
			found = true
			if res.EntityKind == "" {
				t.Errorf("EntityKind empty for match with EntityName=%q", res.EntityName)
			}
			if res.EntityKey == "" {
				t.Errorf("EntityKey empty for match with EntityName=%q", res.EntityName)
			}
		}
	}
	if !found {
		t.Errorf("no results had EntityName populated; expected entity context")
	}
}

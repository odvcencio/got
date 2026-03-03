package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/repo"
)

// TestWriteJSON verifies writeJSON produces pretty-printed JSON with the correct structure.
func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	if err := writeJSON(&buf, data); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "\"key\": \"value\"") {
		t.Fatalf("expected pretty-printed JSON, got: %s", got)
	}
	// Verify it's valid JSON.
	var parsed map[string]string
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Fatalf("parsed key = %q, want %q", parsed["key"], "value")
	}
}

// TestWriteJSON_Struct verifies writeJSON works with a typed struct and camelCase tags.
func TestWriteJSON_Struct(t *testing.T) {
	var buf bytes.Buffer
	data := JSONStatusOutput{
		Branch:    "main",
		NoCommits: true,
	}
	if err := writeJSON(&buf, data); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	var parsed JSONStatusOutput
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Branch != "main" {
		t.Fatalf("branch = %q, want %q", parsed.Branch, "main")
	}
	if !parsed.NoCommits {
		t.Fatal("noCommits = false, want true")
	}
}

// TestStatusCmd_JSON tests the --json flag on the status command.
func TestStatusCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Create a file and stage it.
	writeTestFile(t, filepath.Join(dir, "hello.txt"), []byte("hello\n"))
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.Branch != "main" {
		t.Errorf("branch = %q, want %q", result.Branch, "main")
	}
	if !result.NoCommits {
		t.Error("noCommits = false, want true (no commits yet)")
	}
	if len(result.Staged) != 1 {
		t.Fatalf("len(staged) = %d, want 1", len(result.Staged))
	}
	if result.Staged[0].Path != "hello.txt" {
		t.Errorf("staged[0].path = %q, want %q", result.Staged[0].Path, "hello.txt")
	}
	if result.Staged[0].Status != "new" {
		t.Errorf("staged[0].status = %q, want %q", result.Staged[0].Status, "new")
	}
}

// TestStatusCmd_JSON_WithUntracked tests --json shows untracked files.
func TestStatusCmd_JSON_WithUntracked(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "untracked.txt"), []byte("data\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Untracked) != 1 {
		t.Fatalf("len(untracked) = %d, want 1", len(result.Untracked))
	}
	if result.Untracked[0] != "untracked.txt" {
		t.Errorf("untracked[0] = %q, want %q", result.Untracked[0], "untracked.txt")
	}
}

// TestStatusCmd_JSON_CleanState tests --json after a commit (clean state).
func TestStatusCmd_JSON_CleanState(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial commit", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.NoCommits {
		t.Error("noCommits = true, want false after commit")
	}
	if len(result.Staged) != 0 {
		t.Errorf("len(staged) = %d, want 0", len(result.Staged))
	}
	if len(result.Unstaged) != 0 {
		t.Errorf("len(unstaged) = %d, want 0", len(result.Unstaged))
	}
	if len(result.Untracked) != 0 {
		t.Errorf("len(untracked) = %d, want 0", len(result.Untracked))
	}
}

// TestDiffCmd_JSON_Staged tests --json --staged flag on the diff command.
func TestDiffCmd_JSON_Staged(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Create initial commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("line one\nline two\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Modify and stage.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("line one\nline two modified\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDiffCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--staged", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDiffOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(result.Files))
	}
	f := result.Files[0]
	if f.Path != "file.txt" {
		t.Errorf("file.path = %q, want %q", f.Path, "file.txt")
	}
	if f.Status != "modified" {
		t.Errorf("file.status = %q, want %q", f.Status, "modified")
	}
	if len(f.Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	// Check that hunks contain the expected lines.
	foundDelete := false
	foundAdd := false
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Type == "delete" && l.Content == "line two" {
				foundDelete = true
			}
			if l.Type == "add" && l.Content == "line two modified" {
				foundAdd = true
			}
		}
	}
	if !foundDelete {
		t.Error("expected a deleted line 'line two'")
	}
	if !foundAdd {
		t.Error("expected an added line 'line two modified'")
	}
}

// TestLogCmd_JSON tests --json flag on the log command.
func TestLogCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("a\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("first commit", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "b.txt"), []byte("b\n"))
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("second commit", "bob"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newLogCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONLogOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(result.Commits))
	}

	// Most recent first.
	if result.Commits[0].Message != "second commit" {
		t.Errorf("commits[0].message = %q, want %q", result.Commits[0].Message, "second commit")
	}
	if result.Commits[0].Author != "bob" {
		t.Errorf("commits[0].author = %q, want %q", result.Commits[0].Author, "bob")
	}
	if result.Commits[1].Message != "first commit" {
		t.Errorf("commits[1].message = %q, want %q", result.Commits[1].Message, "first commit")
	}
	// Verify hash fields are populated.
	if result.Commits[0].Hash == "" {
		t.Error("commits[0].hash is empty")
	}
	if result.Commits[0].ShortHash == "" {
		t.Error("commits[0].shortHash is empty")
	}
	// HEAD commit should have a decoration.
	if result.Commits[0].Decoration == "" {
		t.Error("commits[0].decoration is empty, expected HEAD decoration")
	}
}

// TestShowCmd_JSON tests --json flag on the show command.
func TestShowCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("test commit", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newShowCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONShowOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.Hash != string(commitHash) {
		t.Errorf("hash = %q, want %q", result.Hash, commitHash)
	}
	if result.Author != "alice" {
		t.Errorf("author = %q, want %q", result.Author, "alice")
	}
	if result.Message != "test commit" {
		t.Errorf("message = %q, want %q", result.Message, "test commit")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(result.Changes))
	}
	if result.Changes[0].Path != "file.txt" {
		t.Errorf("changes[0].path = %q, want %q", result.Changes[0].Path, "file.txt")
	}
	if result.Changes[0].Status != "A" {
		t.Errorf("changes[0].status = %q, want %q", result.Changes[0].Status, "A")
	}
}

// TestBlameCmd_JSON tests --json flag on the blame command.
func TestBlameCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	source := []byte("package main\n\nfunc target() int { return 1 }\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), source)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial target", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	key := jsonTestDeclarationKey(t, "main.go", source, "target")

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newBlameCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "main.go::" + key, "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONBlameOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.EntityKey != key {
		t.Errorf("entityKey = %q, want %q", result.EntityKey, key)
	}
	if result.Author != "alice" {
		t.Errorf("author = %q, want %q", result.Author, "alice")
	}
	if result.CommitHash != string(commitHash) {
		t.Errorf("commitHash = %q, want %q", result.CommitHash, commitHash)
	}
	if result.Message != "initial target" {
		t.Errorf("message = %q, want %q", result.Message, "initial target")
	}
	if result.Path != "main.go" {
		t.Errorf("path = %q, want %q", result.Path, "main.go")
	}
}

// TestConflictsCmd_JSON_NoConflicts tests --json on conflicts when there are no conflicts.
func TestConflictsCmd_JSON_NoConflicts(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newConflictsCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONConflictsOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 0 {
		t.Errorf("len(files) = %d, want 0", len(result.Files))
	}
}

// TestDiffCmd_JSON_NewFile tests --json for a newly added file (no HEAD yet).
func TestDiffCmd_JSON_NewFile(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "new.txt"), []byte("new content\n"))
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDiffCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--staged", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDiffOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].Status != "added" {
		t.Errorf("files[0].status = %q, want %q", result.Files[0].Status, "added")
	}
}

// TestStatusCmd_JSON_NoHumanOutput verifies --json suppresses human-readable output.
func TestStatusCmd_JSON_NoHumanOutput(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	// JSON output should not contain human-readable markers.
	if strings.Contains(raw, "on main") && !strings.Contains(raw, "\"branch\"") {
		t.Errorf("output contains human-readable text: %s", raw)
	}
	// Should start with { (JSON object).
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Errorf("output does not start with '{': %s", raw)
	}
}

// --- helpers ---

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func jsonTestDeclarationKey(t *testing.T, path string, source []byte, name string) string {
	t.Helper()
	el, err := entity.Extract(path, source)
	if err != nil {
		t.Fatalf("entity.Extract(%s): %v", path, err)
	}
	for i := range el.Entities {
		if el.Entities[i].Name == name {
			return el.Entities[i].IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %s", name, path)
	return ""
}

package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestModuleImpact_NoModule verifies that ModuleImpact returns an error
// when the module does not exist.
func TestModuleImpact_NoModule(t *testing.T) {
	r := createTestRepo(t)

	_, err := r.ModuleImpact("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent module")
	}
}

// TestModuleImpact_NoLockedCommit verifies that ModuleImpact returns an
// error when the module has no locked commit.
func TestModuleImpact_NoLockedCommit(t *testing.T) {
	r := createTestRepo(t)

	// Add module without locking it.
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	_, err := r.ModuleImpact("mylib")
	if err == nil {
		t.Fatal("expected error for unlocked module")
	}
}

// TestModuleImpact_SameCommit verifies that when old and new commits are
// the same, the report is empty.
func TestModuleImpact_SameCommit(t *testing.T) {
	r := createTestRepo(t)

	// Create a commit in the repo to use as the module commit.
	goFile := "package mylib\n\nfunc Helper() string { return \"hello\" }\n"
	impactWriteFile(t, r.RootDir, "lib.go", goFile)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial lib", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Add module and lock it.
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	if err := r.UpdateModuleLock("mylib", commitHash, "https://example.com/mylib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Write HEAD file (simulating sync) with the same commit.
	metaDir := r.ModuleMetadataDir("mylib")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(commitHash)+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	report, err := r.ModuleImpact("mylib")
	if err != nil {
		t.Fatalf("ModuleImpact: %v", err)
	}

	if len(report.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(report.Changes))
	}
	if len(report.Impacted) != 0 {
		t.Errorf("expected 0 impacted, got %d", len(report.Impacted))
	}
}

// TestModuleImpact_DetectsEntityChanges verifies that entity-level diffs
// between old and new module commits are correctly detected.
func TestModuleImpact_DetectsEntityChanges(t *testing.T) {
	r := createTestRepo(t)

	// First commit: module has one function.
	src1 := "package mylib\n\nfunc Helper() string { return \"hello\" }\n"
	impactWriteFile(t, r.RootDir, "lib.go", src1)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	commit1, err := r.Commit("v1", "tester")
	if err != nil {
		t.Fatalf("Commit v1: %v", err)
	}

	// Second commit: modify the function and add a new one.
	src2 := "package mylib\n\nfunc Helper() string { return \"hi\" }\n\nfunc NewFunc() int { return 42 }\n"
	impactWriteFile(t, r.RootDir, "lib.go", src2)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	commit2, err := r.Commit("v2", "tester")
	if err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	// Set up module pointing at commit2 with HEAD at commit1.
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	if err := r.UpdateModuleLock("mylib", commit2, "https://example.com/mylib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Write module HEAD as commit1 (the "old" version).
	metaDir := r.ModuleMetadataDir("mylib")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(commit1)+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	report, err := r.ModuleImpact("mylib")
	if err != nil {
		t.Fatalf("ModuleImpact: %v", err)
	}

	if report.ModuleName != "mylib" {
		t.Errorf("ModuleName = %q, want %q", report.ModuleName, "mylib")
	}

	// We expect at least 2 changes: Helper modified, NewFunc added.
	if len(report.Changes) < 2 {
		t.Errorf("expected at least 2 changes, got %d: %+v", len(report.Changes), report.Changes)
	}

	// Verify we have one "modified" and one "added" change.
	var hasModified, hasAdded bool
	for _, c := range report.Changes {
		switch c.ChangeType {
		case "modified":
			if c.EntityName == "Helper" {
				hasModified = true
			}
		case "added":
			if c.EntityName == "NewFunc" {
				hasAdded = true
			}
		}
	}
	if !hasModified {
		t.Errorf("expected modified change for Helper, changes: %+v", report.Changes)
	}
	if !hasAdded {
		t.Errorf("expected added change for NewFunc, changes: %+v", report.Changes)
	}
}

// TestModuleImpact_FindsImpactedEntities verifies that entities in the parent
// repo that reference changed module entities are detected.
func TestModuleImpact_FindsImpactedEntities(t *testing.T) {
	r := createTestRepo(t)

	// Create initial parent repo commit with a file that references "Helper".
	parentSrc := "package main\n\nimport \"mylib\"\n\nfunc main() {\n\tresult := mylib.Helper()\n\t_ = result\n}\n"
	impactWriteFile(t, r.RootDir, "main.go", parentSrc)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go: %v", err)
	}
	_, err := r.Commit("parent initial", "tester")
	if err != nil {
		t.Fatalf("Commit parent: %v", err)
	}

	// Module version 1: Helper function.
	src1 := "package mylib\n\nfunc Helper() string { return \"hello\" }\n"
	impactWriteFile(t, r.RootDir, "lib.go", src1)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add lib v1: %v", err)
	}
	commit1, err := r.Commit("module v1", "tester")
	if err != nil {
		t.Fatalf("Commit module v1: %v", err)
	}

	// Module version 2: modified Helper.
	src2 := "package mylib\n\nfunc Helper() string { return \"hi there\" }\n"
	impactWriteFile(t, r.RootDir, "lib.go", src2)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add lib v2: %v", err)
	}
	commit2, err := r.Commit("module v2", "tester")
	if err != nil {
		t.Fatalf("Commit module v2: %v", err)
	}

	// Set up module pointing at commit2 with HEAD at commit1.
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	if err := r.UpdateModuleLock("mylib", commit2, "https://example.com/mylib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Write module HEAD as commit1.
	metaDir := r.ModuleMetadataDir("mylib")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(commit1)+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	report, err := r.ModuleImpact("mylib")
	if err != nil {
		t.Fatalf("ModuleImpact: %v", err)
	}

	// Should have changes in the module.
	if len(report.Changes) == 0 {
		t.Fatal("expected changes in module, got 0")
	}

	// Should find impacted entities in the parent that reference "Helper".
	if len(report.Impacted) == 0 {
		t.Fatal("expected impacted entities, got 0")
	}

	// At least one impacted entry should reference main.go.
	foundMainGo := false
	for _, imp := range report.Impacted {
		if imp.FilePath == "main.go" {
			foundMainGo = true
			if imp.Reason == "" {
				t.Error("impacted entity has empty reason")
			}
			break
		}
	}
	if !foundMainGo {
		t.Errorf("expected impacted entity in main.go, got: %+v", report.Impacted)
	}
}

// TestModuleImpact_InitialCommit verifies that when there is no old commit
// (first time), all entities are treated as "added".
func TestModuleImpact_InitialCommit(t *testing.T) {
	r := createTestRepo(t)

	// Create a commit to use as module content.
	src := "package mylib\n\nfunc Alpha() {}\n\nfunc Beta() {}\n"
	impactWriteFile(t, r.RootDir, "lib.go", src)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Set up module with lock but no HEAD (no previous checkout).
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	if err := r.UpdateModuleLock("mylib", commitHash, "https://example.com/mylib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	report, err := r.ModuleImpact("mylib")
	if err != nil {
		t.Fatalf("ModuleImpact: %v", err)
	}

	// All entities should be "added".
	for _, c := range report.Changes {
		if c.ChangeType != "added" {
			t.Errorf("expected all changes to be 'added', got %q for %s", c.ChangeType, c.EntityName)
		}
	}

	// Should have at least Alpha and Beta.
	names := make(map[string]bool)
	for _, c := range report.Changes {
		names[c.EntityName] = true
	}
	if !names["Alpha"] {
		t.Error("expected Alpha in changes")
	}
	if !names["Beta"] {
		t.Error("expected Beta in changes")
	}
}

// TestModuleImpact_RemovedEntity verifies detection of removed entities.
func TestModuleImpact_RemovedEntity(t *testing.T) {
	r := createTestRepo(t)

	// V1: two functions.
	src1 := "package mylib\n\nfunc Keep() {}\n\nfunc Remove() {}\n"
	impactWriteFile(t, r.RootDir, "lib.go", src1)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	commit1, err := r.Commit("v1", "tester")
	if err != nil {
		t.Fatalf("Commit v1: %v", err)
	}

	// V2: Remove one function.
	src2 := "package mylib\n\nfunc Keep() {}\n"
	impactWriteFile(t, r.RootDir, "lib.go", src2)
	if err := r.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	commit2, err := r.Commit("v2", "tester")
	if err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	// Set up module.
	entry := ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	if err := r.UpdateModuleLock("mylib", commit2, "https://example.com/mylib.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	metaDir := r.ModuleMetadataDir("mylib")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(commit1)+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	report, err := r.ModuleImpact("mylib")
	if err != nil {
		t.Fatalf("ModuleImpact: %v", err)
	}

	// Should have a "removed" change for Remove.
	var foundRemoved bool
	for _, c := range report.Changes {
		if c.ChangeType == "removed" && c.EntityName == "Remove" {
			foundRemoved = true
			break
		}
	}
	if !foundRemoved {
		t.Errorf("expected removed change for Remove, got: %+v", report.Changes)
	}
}

// TestContainsReference verifies word-boundary-aware name matching.
func TestContainsReference(t *testing.T) {
	tests := []struct {
		content string
		name    string
		want    bool
	}{
		{"result := Helper()", "Helper", true},
		{"mylib.Helper(x)", "Helper", true},
		{"// Helper is useful", "Helper", true},
		{"HelperFunc()", "Helper", false},        // Helper is prefix, not word
		{"BigHelper()", "Helper", false},         // Helper is suffix, not word
		{"_Helper_", "Helper", false},            // underscore is an ident char
		{"func DoWork() {}", "DoWork", true},     // exact match
		{"func DoWorkNow() {}", "DoWork", false}, // DoWork is prefix
		{"", "Helper", false},                    // empty content
		{"x := Helper\n", "Helper", true},        // at end of line
		{"Helper", "Helper", true},               // exact content
	}

	for _, tt := range tests {
		got := containsReference(tt.content, tt.name)
		if got != tt.want {
			t.Errorf("containsReference(%q, %q) = %v, want %v", tt.content, tt.name, got, tt.want)
		}
	}
}

// TestDeduplicateImpacted verifies deduplication of impacted entries.
func TestDeduplicateImpacted(t *testing.T) {
	items := []ImpactedEntity{
		{EntityKey: "declaration:main", FilePath: "main.go", Reason: "reason1"},
		{EntityKey: "declaration:main", FilePath: "main.go", Reason: "reason2"},
		{EntityKey: "declaration:other", FilePath: "main.go", Reason: "reason3"},
	}
	got := deduplicateImpacted(items)
	if len(got) != 2 {
		t.Errorf("expected 2 after dedup, got %d: %+v", len(got), got)
	}
}

// impactWriteFile is a test helper that writes content to a file inside the repo.
func impactWriteFile(t *testing.T, rootDir, name, content string) {
	t.Helper()
	p := filepath.Join(rootDir, name)
	writeFile(t, p, []byte(content))
}

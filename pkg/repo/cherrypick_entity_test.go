package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

func TestCherryPickEntity_AppliesOnlySelectedEntityDelta(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	base := []byte("package main\n\nfunc helper() int { return 1 }\n\nfunc target() int { return 1 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), base)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	key := cherryPickDeclarationKey(t, "main.go", base, "target")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	targetVersion := []byte("package main\n\nfunc helper() int { return 2 }\n\nfunc target() int { return 2 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), targetVersion)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(target): %v", err)
	}
	targetHash, err := r.Commit("update helper and target", "bob")
	if err != nil {
		t.Fatalf("Commit(target): %v", err)
	}

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	result, err := r.CherryPickEntity("main.go::"+key, targetHash)
	if err != nil {
		t.Fatalf("CherryPickEntity: %v", err)
	}
	if result.Path != "main.go" {
		t.Fatalf("Path = %q, want %q", result.Path, "main.go")
	}
	if result.EntityKey != key {
		t.Fatalf("EntityKey = %q, want %q", result.EntityKey, key)
	}

	got, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(got)
	if !strings.Contains(text, "func helper() int { return 1 }") {
		t.Fatalf("helper unexpectedly changed:\n%s", text)
	}
	if !strings.Contains(text, "func target() int { return 2 }") {
		t.Fatalf("target delta not applied:\n%s", text)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != result.CommitHash {
		t.Fatalf("HEAD = %s, want %s", headHash, result.CommitHash)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		t.Fatalf("ReadCommit(HEAD): %v", err)
	}
	if len(headCommit.Parents) != 1 || headCommit.Parents[0] != baseHash {
		t.Fatalf("new commit parents = %v, want [%s]", headCommit.Parents, baseHash)
	}
}

func TestCherryPickEntity_TargetDoesNotChangeSelectedEntity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	base := []byte("package main\n\nfunc helper() int { return 1 }\n\nfunc target() int { return 1 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), base)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	key := cherryPickDeclarationKey(t, "main.go", base, "target")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	helperOnly := []byte("package main\n\nfunc helper() int { return 9 }\n\nfunc target() int { return 1 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), helperOnly)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(helperOnly): %v", err)
	}
	targetHash, err := r.Commit("touch helper only", "bob")
	if err != nil {
		t.Fatalf("Commit(helperOnly): %v", err)
	}

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	_, err = r.CherryPickEntity("main.go::"+key, targetHash)
	if err == nil {
		t.Fatal("CherryPickEntity should fail when selected entity is unchanged")
	}
	if !strings.Contains(err.Error(), "does not change") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "does not change")
	}
}

func TestCherryPickEntity_ConflictWhenHeadDiverged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	base := []byte("package main\n\nfunc target() int { return 1 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), base)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	key := cherryPickDeclarationKey(t, "main.go", base, "target")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	theirs := []byte("package main\n\nfunc target() int { return 2 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), theirs)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(theirs): %v", err)
	}
	targetHash, err := r.Commit("target change", "bob")
	if err != nil {
		t.Fatalf("Commit(theirs): %v", err)
	}

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	ours := []byte("package main\n\nfunc target() int { return 9 }\n")
	cherryPickWriteFile(t, filepath.Join(dir, "main.go"), ours)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(ours): %v", err)
	}
	if _, err := r.Commit("main diverged", "carol"); err != nil {
		t.Fatalf("Commit(ours): %v", err)
	}

	_, err = r.CherryPickEntity("main.go::"+key, targetHash)
	if err == nil {
		t.Fatal("CherryPickEntity should fail on conflicting change")
	}
	if !strings.Contains(err.Error(), "conflict applying") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "conflict applying")
	}

	got, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	if !strings.Contains(string(got), "return 9") {
		t.Fatalf("working tree should keep HEAD content on failed cherry-pick, got:\n%s", string(got))
	}
}

func cherryPickDeclarationKey(t *testing.T, path string, source []byte, name string) string {
	t.Helper()

	el, err := entity.Extract(path, source)
	if err != nil {
		t.Fatalf("entity.Extract(%q): %v", path, err)
	}
	for i := range el.Entities {
		if el.Entities[i].Name == name {
			return el.Entities[i].IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %s", name, path)
	return ""
}

func cherryPickWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

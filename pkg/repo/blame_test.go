package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

func TestBlameEntity_FindsMostRecentEntityChange(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source1 := []byte("package main\n\nfunc helper() int { return 1 }\n\nfunc target() int { return 1 }\n")
	source2 := []byte("package main\n\nfunc helper() int { return 2 }\n\nfunc target() int { return 1 }\n")
	source3 := []byte("package main\n\nfunc helper() int { return 2 }\n\nfunc target() int { return 3 }\n")

	writeFile(t, filepath.Join(dir, "main.go"), source1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add source1: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit source1: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), source2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add source2: %v", err)
	}
	if _, err := r.Commit("update helper", "bob"); err != nil {
		t.Fatalf("Commit source2: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), source3)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add source3: %v", err)
	}
	wantHash, err := r.Commit("update target", "carol")
	if err != nil {
		t.Fatalf("Commit source3: %v", err)
	}

	key := mustDeclarationKey(t, "main.go", source3, "target")
	result, err := r.BlameEntity("main.go::"+key, 20)
	if err != nil {
		t.Fatalf("BlameEntity: %v", err)
	}

	if result.EntityKey != key {
		t.Fatalf("EntityKey = %q, want %q", result.EntityKey, key)
	}
	if result.Author != "carol" {
		t.Fatalf("Author = %q, want %q", result.Author, "carol")
	}
	if result.CommitHash != wantHash {
		t.Fatalf("CommitHash = %q, want %q", result.CommitHash, wantHash)
	}
	if result.Message != "update target" {
		t.Fatalf("Message = %q, want %q", result.Message, "update target")
	}
}

func TestBlameEntity_FollowsKeyShiftContinuity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source1 := []byte("package main\n\nfunc Target() int {\n\treturn 1\n}\n")
	source2 := []byte("package main\n\nfunc Target() int {\n\treturn 99\n}\n\nfunc Target() int {\n\treturn 1\n}\n")
	source3 := []byte("package main\n\nfunc Target() int {\n\treturn 99\n}\n\nfunc Target() int {\n\treturn 1\n}\n\nfunc helper() int {\n\treturn 1\n}\n")

	writeFile(t, filepath.Join(dir, "main.go"), source1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(source1): %v", err)
	}
	wantHash, err := r.Commit("initial target", "alice")
	if err != nil {
		t.Fatalf("Commit(source1): %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), source2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(source2): %v", err)
	}
	if _, err := r.Commit("shift key only", "bob"); err != nil {
		t.Fatalf("Commit(source2): %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), source3)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(source3): %v", err)
	}
	if _, err := r.Commit("touch helper only", "carol"); err != nil {
		t.Fatalf("Commit(source3): %v", err)
	}

	key := mustDeclarationKeyByBody(t, "main.go", source3, "Target", "return 1")
	result, err := r.BlameEntity("main.go::"+key, 20)
	if err != nil {
		t.Fatalf("BlameEntity: %v", err)
	}

	if result.Author != "alice" {
		t.Fatalf("Author = %q, want %q", result.Author, "alice")
	}
	if result.CommitHash != wantHash {
		t.Fatalf("CommitHash = %q, want %q", result.CommitHash, wantHash)
	}
	if result.Message != "initial target" {
		t.Fatalf("Message = %q, want %q", result.Message, "initial target")
	}
}

func TestBlameEntity_FollowsMoveContinuity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldPath := "old/main.go"
	newPath := "new/main.go"
	source := []byte("package main\n\nfunc Target() int {\n\treturn 1\n}\n")

	writeFile(t, filepath.Join(dir, filepath.FromSlash(oldPath)), source)
	if err := r.Add([]string{oldPath}); err != nil {
		t.Fatalf("Add(%s): %v", oldPath, err)
	}
	wantHash, err := r.Commit("add old path target", "alice")
	if err != nil {
		t.Fatalf("Commit(add old path target): %v", err)
	}

	oldAbs := filepath.Join(dir, filepath.FromSlash(oldPath))
	newAbs := filepath.Join(dir, filepath.FromSlash(newPath))
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll(new path): %v", err)
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		t.Fatalf("Rename(%s -> %s): %v", oldPath, newPath, err)
	}
	if err := r.Add([]string{newPath}); err != nil {
		t.Fatalf("Add(%s): %v", newPath, err)
	}
	if err := r.Remove([]string{oldPath}, true); err != nil {
		t.Fatalf("Remove(%s): %v", oldPath, err)
	}
	if _, err := r.Commit("move target path", "bob"); err != nil {
		t.Fatalf("Commit(move target path): %v", err)
	}

	key := mustDeclarationKeyByBody(t, newPath, source, "Target", "return 1")
	result, err := r.BlameEntity(newPath+"::"+key, 20)
	if err != nil {
		t.Fatalf("BlameEntity: %v", err)
	}

	if result.Author != "alice" {
		t.Fatalf("Author = %q, want %q", result.Author, "alice")
	}
	if result.CommitHash != wantHash {
		t.Fatalf("CommitHash = %q, want %q", result.CommitHash, wantHash)
	}
	if result.Message != "add old path target" {
		t.Fatalf("Message = %q, want %q", result.Message, "add old path target")
	}
}

func TestBlameEntity_NotFound(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	_, err := r.BlameEntity("main.go::decl:function_declaration::missing:-:0", 10)
	if err == nil {
		t.Fatal("BlameEntity should fail for missing entity key")
	}
	if !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("error = %v, want ErrEntityNotFound", err)
	}
	if !strings.Contains(err.Error(), "entity not found") {
		t.Fatalf("error %q should include \"entity not found\"", err)
	}
}

func TestBlameEntity_InvalidSelector(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	_, err := r.BlameEntity("main.go", 10)
	if err == nil {
		t.Fatal("BlameEntity should fail for invalid selector")
	}
	if !errors.Is(err, ErrInvalidEntitySelector) {
		t.Fatalf("error = %v, want ErrInvalidEntitySelector", err)
	}
}

func mustDeclarationKey(t *testing.T, path string, source []byte, name string) string {
	t.Helper()
	el, err := entity.Extract(path, source)
	if err != nil {
		t.Fatalf("entity.Extract(%s): %v", path, err)
	}

	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Name == name {
			return e.IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %s", name, path)
	return ""
}

func mustDeclarationKeyByBody(t *testing.T, path string, source []byte, name, bodyNeedle string) string {
	t.Helper()
	el, err := entity.Extract(path, source)
	if err != nil {
		t.Fatalf("entity.Extract(%s): %v", path, err)
	}

	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Kind != entity.KindDeclaration || e.Name != name {
			continue
		}
		if strings.Contains(string(e.Body), bodyNeedle) {
			return e.IdentityKey()
		}
	}
	t.Fatalf("declaration %q with body containing %q not found in %s", name, bodyNeedle, path)
	return ""
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

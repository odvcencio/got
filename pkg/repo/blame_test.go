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

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/repo"
)

func TestBlameCmd_EntityAttributionOutput(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	source := []byte("package main\n\nfunc target() int { return 1 }\n")
	writeCmdBlameFile(t, filepath.Join(dir, "main.go"), source)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial target", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	key := cmdBlameDeclarationKey(t, "main.go", source, "target")

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newBlameCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "main.go::" + key})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := fmt.Sprintf("%s\t%s\t%s\t%s\n", key, "alice", commitHash, "initial target")
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestBlameCmd_EntityNotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeCmdBlameFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newBlameCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "main.go::decl:function_definition::missing:-:0"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute should fail when entity key is missing")
	}
	if !strings.Contains(err.Error(), "entity not found") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "entity not found")
	}
}

func writeCmdBlameFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd %s: %v", wd, err)
		}
	}
}

func cmdBlameDeclarationKey(t *testing.T, path string, source []byte, name string) string {
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

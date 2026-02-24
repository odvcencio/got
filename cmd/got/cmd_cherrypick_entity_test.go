package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/repo"
)

func TestCherryPickCmd_EntityAppliesSelectedDelta(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	base := []byte("package main\n\nfunc helper() int { return 1 }\n\nfunc target() int { return 1 }\n")
	writeCherryPickCmdFile(t, filepath.Join(dir, "main.go"), base)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}
	key := cherryPickCmdDeclarationKey(t, "main.go", base, "target")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	targetVersion := []byte("package main\n\nfunc helper() int { return 2 }\n\nfunc target() int { return 2 }\n")
	writeCherryPickCmdFile(t, filepath.Join(dir, "main.go"), targetVersion)
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

	restore := chdirForCherryPickCmdTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCherryPickCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--entity", "main.go::" + key, string(targetHash)})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "cherry-pick") {
		t.Fatalf("output = %q, want to contain %q", out.String(), "cherry-pick")
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
		t.Fatalf("target change missing:\n%s", text)
	}
}

func TestCherryPickCmd_EntityNotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	v1 := []byte("package main\n\nfunc target() int { return 1 }\n")
	writeCherryPickCmdFile(t, filepath.Join(dir, "main.go"), v1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(v1): %v", err)
	}
	if _, err := r.Commit("v1", "alice"); err != nil {
		t.Fatalf("Commit(v1): %v", err)
	}

	v2 := []byte("package main\n\nfunc target() int { return 2 }\n")
	writeCherryPickCmdFile(t, filepath.Join(dir, "main.go"), v2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(v2): %v", err)
	}
	targetHash, err := r.Commit("v2", "bob")
	if err != nil {
		t.Fatalf("Commit(v2): %v", err)
	}

	restore := chdirForCherryPickCmdTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCherryPickCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--entity", "main.go::decl:function_definition::missing:-:0", string(targetHash)})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute should fail for missing entity key")
	}
	if !strings.Contains(err.Error(), "entity not found") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "entity not found")
	}
}

func writeCherryPickCmdFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func cherryPickCmdDeclarationKey(t *testing.T, path string, source []byte, name string) string {
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

func chdirForCherryPickCmdTest(t *testing.T, dir string) func() {
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

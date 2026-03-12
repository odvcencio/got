package coord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildXrefIndex(t *testing.T) {
	// Create a workspace directory with Go files that import an external package.
	dir := t.TempDir()

	// go.mod
	gomod := `module github.com/example/consumer

go 1.25

require github.com/example/provider v1.0.0
`
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)

	// Source file that imports and calls functions from provider
	os.MkdirAll(filepath.Join(dir, "cmd"), 0o755)
	src := `package main

import (
	"fmt"
	"github.com/example/provider/pkg/handler"
	"github.com/example/provider/pkg/util"
)

func main() {
	result := handler.HandleRequest("world")
	fmt.Println(result)
	name := util.FormatName("John", "Doe")
	fmt.Println(name)
}

func doStuff() {
	handler.HandleRequest("test")
}
`
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte(src), 0o644)

	idx, err := BuildXrefIndex(dir, "github.com/example/consumer")
	if err != nil {
		t.Fatalf("BuildXrefIndex: %v", err)
	}

	// Check handler.HandleRequest references
	handleRefs, ok := idx.Refs["github.com/example/provider/pkg/handler.HandleRequest"]
	if !ok {
		t.Fatal("expected refs for handler.HandleRequest")
	}

	if len(handleRefs) != 2 {
		t.Fatalf("expected 2 refs for HandleRequest, got %d", len(handleRefs))
	}

	// Verify the calls come from the right functions
	foundMain := false
	foundDoStuff := false
	for _, ref := range handleRefs {
		if ref.Entity == "main" {
			foundMain = true
		}
		if ref.Entity == "doStuff" {
			foundDoStuff = true
		}
		if ref.File != "cmd/main.go" {
			t.Errorf("unexpected file: %s", ref.File)
		}
	}
	if !foundMain {
		t.Error("expected HandleRequest call from main()")
	}
	if !foundDoStuff {
		t.Error("expected HandleRequest call from doStuff()")
	}

	// Check util.FormatName references
	fmtRefs, ok := idx.Refs["github.com/example/provider/pkg/util.FormatName"]
	if !ok {
		t.Fatal("expected refs for util.FormatName")
	}
	if len(fmtRefs) != 1 {
		t.Fatalf("expected 1 ref for FormatName, got %d", len(fmtRefs))
	}
	if fmtRefs[0].Entity != "main" {
		t.Errorf("FormatName caller = %q, want main", fmtRefs[0].Entity)
	}
}

func TestBuildXrefIndex_ImportAlias(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module github.com/test/app
go 1.25
`), 0o644)

	// Source with import alias
	src := `package main

import (
	h "github.com/example/provider/pkg/handler"
)

func process() {
	h.HandleRequest("aliased")
}
`
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)

	idx, err := BuildXrefIndex(dir, "github.com/test/app")
	if err != nil {
		t.Fatalf("BuildXrefIndex: %v", err)
	}

	refs, ok := idx.Refs["github.com/example/provider/pkg/handler.HandleRequest"]
	if !ok {
		t.Fatal("expected refs for aliased import handler.HandleRequest")
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Entity != "process" {
		t.Errorf("entity = %q, want process", refs[0].Entity)
	}
}

func TestBuildXrefIndex_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	idx, err := BuildXrefIndex(dir, "github.com/test/empty")
	if err != nil {
		t.Fatalf("BuildXrefIndex: %v", err)
	}
	if len(idx.Refs) != 0 {
		t.Errorf("expected no refs, got %d", len(idx.Refs))
	}
}

func TestSaveLoadXrefIndex(t *testing.T) {
	c := newTestCoordinator(t)

	idx := &XrefIndex{
		Refs: map[string][]XrefCallSite{
			"github.com/example/provider/pkg/handler.HandleRequest": {
				{File: "cmd/main.go", Entity: "main", Line: 10},
			},
		},
	}

	if err := c.SaveXrefIndex(idx); err != nil {
		t.Fatalf("SaveXrefIndex: %v", err)
	}

	loaded, err := c.LoadXrefIndex()
	if err != nil {
		t.Fatalf("LoadXrefIndex: %v", err)
	}

	refs, ok := loaded.Refs["github.com/example/provider/pkg/handler.HandleRequest"]
	if !ok {
		t.Fatal("expected refs in loaded index")
	}
	if len(refs) != 1 || refs[0].Entity != "main" {
		t.Errorf("loaded refs = %v", refs)
	}
}

package coord

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

// newTestRepoWithCommit creates a test repo with committed Go files.
func newTestRepoWithCommit(t *testing.T, files map[string]string) *repo.Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	var paths []string
	for name, content := range files {
		fullPath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
		paths = append(paths, name)
	}

	if err := r.Add(paths); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("test commit", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	return r
}

func TestBuildExportIndex(t *testing.T) {
	r := newTestRepoWithCommit(t, map[string]string{
		"pkg/handler/handler.go": `package handler

import "fmt"

// HandleRequest processes an incoming request.
func HandleRequest(name string) string {
	return fmt.Sprintf("hello %s", name)
}

// helperFunc is unexported.
func helperFunc() {}

// Server is an exported type.
type Server struct {
	Port int
}

// Start is an exported method on Server.
func (s *Server) Start() error {
	return nil
}

// MaxRetries is an exported constant.
const MaxRetries = 3

// internalLimit is unexported.
const internalLimit = 10

// DefaultTimeout is an exported var.
var DefaultTimeout = 30
`,
		"pkg/util/util.go": `package util

// FormatName formats a name.
func FormatName(first, last string) string {
	return first + " " + last
}

// lowercase is unexported.
func lowercase() {}
`,
	})

	idx, err := BuildExportIndex(r)
	if err != nil {
		t.Fatalf("BuildExportIndex: %v", err)
	}

	// Check handler package
	handlerPkg, ok := idx.Packages["pkg/handler"]
	if !ok {
		t.Fatal("expected pkg/handler package in index")
	}

	// Check exported function
	if _, ok := handlerPkg["func:HandleRequest"]; !ok {
		t.Error("expected func:HandleRequest in handler package")
	}

	// Check exported type
	if _, ok := handlerPkg["type:Server"]; !ok {
		t.Error("expected type:Server in handler package")
	}

	// Check exported method
	if _, ok := handlerPkg["method:Server.Start"]; !ok {
		t.Error("expected method:Server.Start in handler package")
	}

	// Check exported const
	if _, ok := handlerPkg["const:MaxRetries"]; !ok {
		t.Error("expected const:MaxRetries in handler package")
	}

	// Check exported var
	if _, ok := handlerPkg["var:DefaultTimeout"]; !ok {
		t.Error("expected var:DefaultTimeout in handler package")
	}

	// Check that unexported symbols are NOT in the index
	if _, ok := handlerPkg["func:helperFunc"]; ok {
		t.Error("unexported helperFunc should not be in index")
	}
	if _, ok := handlerPkg["const:internalLimit"]; ok {
		t.Error("unexported internalLimit should not be in index")
	}

	// Check util package
	utilPkg, ok := idx.Packages["pkg/util"]
	if !ok {
		t.Fatal("expected pkg/util package in index")
	}

	if _, ok := utilPkg["func:FormatName"]; !ok {
		t.Error("expected func:FormatName in util package")
	}
	if _, ok := utilPkg["func:lowercase"]; ok {
		t.Error("unexported lowercase should not be in index")
	}

	// Check signatures
	hr := handlerPkg["func:HandleRequest"]
	if hr.Signature != "func HandleRequest(name string) string" {
		t.Errorf("HandleRequest signature = %q", hr.Signature)
	}
	if hr.File != "pkg/handler/handler.go" {
		t.Errorf("HandleRequest file = %q", hr.File)
	}
}

func TestBuildExportIndex_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write a non-Go file and commit
	fpath := filepath.Join(dir, "README.txt")
	os.WriteFile(fpath, []byte("hello"), 0o644)
	r.Add([]string{"README.txt"})
	r.Commit("initial", "test")

	idx, err := BuildExportIndex(r)
	if err != nil {
		t.Fatalf("BuildExportIndex: %v", err)
	}

	if len(idx.Packages) != 0 {
		t.Errorf("expected no packages, got %d", len(idx.Packages))
	}
}

func TestSaveLoadExportIndex(t *testing.T) {
	c := newTestCoordinator(t)

	idx := &ExportIndex{
		Packages: map[string]map[string]ExportedEntity{
			"pkg/handler": {
				"func:HandleRequest": {
					Key:       "func:HandleRequest",
					Signature: "func HandleRequest(name string) string",
					File:      "pkg/handler/handler.go",
					Hash:      "abc123",
				},
			},
		},
	}

	if err := c.SaveExportIndex(idx); err != nil {
		t.Fatalf("SaveExportIndex: %v", err)
	}

	loaded, err := c.LoadExportIndex()
	if err != nil {
		t.Fatalf("LoadExportIndex: %v", err)
	}

	handlerPkg, ok := loaded.Packages["pkg/handler"]
	if !ok {
		t.Fatal("expected pkg/handler in loaded index")
	}

	entity, ok := handlerPkg["func:HandleRequest"]
	if !ok {
		t.Fatal("expected func:HandleRequest in loaded index")
	}
	if entity.Signature != "func HandleRequest(name string) string" {
		t.Errorf("loaded signature = %q", entity.Signature)
	}
}

package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBlameFile_MultipleEntities(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source1 := []byte("package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 2 }\n")
	writeFile(t, filepath.Join(dir, "main.go"), source1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add source1: %v", err)
	}
	hash1, err := r.Commit("add alpha and beta", "alice")
	if err != nil {
		t.Fatalf("Commit source1: %v", err)
	}

	// Update only Beta.
	source2 := []byte("package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 99 }\n")
	writeFile(t, filepath.Join(dir, "main.go"), source2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add source2: %v", err)
	}
	hash2, err := r.Commit("update beta", "bob")
	if err != nil {
		t.Fatalf("Commit source2: %v", err)
	}

	results, err := r.BlameFile("main.go", 20)
	if err != nil {
		t.Fatalf("BlameFile: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Alpha should be attributed to alice (hash1).
	alphaFound := false
	betaFound := false
	for _, res := range results {
		if res.Author == "alice" && res.CommitHash == hash1 && res.Message == "add alpha and beta" {
			alphaFound = true
		}
		if res.Author == "bob" && res.CommitHash == hash2 && res.Message == "update beta" {
			betaFound = true
		}
	}
	if !alphaFound {
		t.Errorf("expected Alpha blamed to alice at %s, results: %+v", hash1, results)
	}
	if !betaFound {
		t.Errorf("expected Beta blamed to bob at %s, results: %+v", hash2, results)
	}
}

func TestBlameFile_SingleEntity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := []byte("package main\n\nfunc OnlyOne() int { return 42 }\n")
	writeFile(t, filepath.Join(dir, "main.go"), source)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	wantHash, err := r.Commit("add only one", "carol")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.BlameFile("main.go", 20)
	if err != nil {
		t.Fatalf("BlameFile: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if results[0].Author != "carol" {
		t.Errorf("Author = %q, want %q", results[0].Author, "carol")
	}
	if results[0].CommitHash != wantHash {
		t.Errorf("CommitHash = %q, want %q", results[0].CommitHash, wantHash)
	}
}

func TestBlameFile_NoDeclarations(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// A file with only a package statement and no declarations.
	source := []byte("package main\n")
	writeFile(t, filepath.Join(dir, "main.go"), source)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add empty file", "dave"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.BlameFile("main.go", 20)
	if err != nil {
		t.Fatalf("BlameFile: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("got %d results, want 0 for file with no declarations", len(results))
	}
}

func TestBlameFile_FileNotInTree(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	_, err := r.BlameFile("nonexistent.go", 20)
	if err == nil {
		t.Fatal("BlameFile should fail for nonexistent file")
	}
}

func TestBlameFile_UnsupportedFileType(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// A plain text file that tree-sitter does not support.
	writeFile(t, filepath.Join(dir, "readme.txt"), []byte("hello world\n"))
	if err := r.Add([]string{"readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add readme", "eve"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	_, err = r.BlameFile("readme.txt", 20)
	if err == nil {
		t.Fatal("BlameFile should fail for unsupported file type")
	}
}

func TestBlameFile_SubdirectoryPath(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := []byte("package sub\n\nfunc SubFunc() string { return \"hello\" }\n")
	subDir := filepath.Join(dir, "pkg", "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(subDir, "sub.go"), source)
	if err := r.Add([]string{"pkg/sub/sub.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	wantHash, err := r.Commit("add sub func", "frank")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := r.BlameFile("pkg/sub/sub.go", 20)
	if err != nil {
		t.Fatalf("BlameFile: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Author != "frank" {
		t.Errorf("Author = %q, want %q", results[0].Author, "frank")
	}
	if results[0].CommitHash != wantHash {
		t.Errorf("CommitHash = %q, want %q", results[0].CommitHash, wantHash)
	}
	if results[0].Path != "pkg/sub/sub.go" {
		t.Errorf("Path = %q, want %q", results[0].Path, "pkg/sub/sub.go")
	}
}

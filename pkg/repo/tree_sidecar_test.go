package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTree_IncludesSidecarDir(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatal(err)
	}

	// Write a .gts/ sidecar file (not staged)
	gtsDir := filepath.Join(r.RootDir, ".gts")
	os.MkdirAll(gtsDir, 0o755)
	os.WriteFile(filepath.Join(gtsDir, "index.json"), []byte(`{"version":"0.2.0"}`), 0o644)

	stg, _ := r.ReadStaging()
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatal(err)
	}

	// Flatten tree and verify .gts/index.json is present
	entries, err := r.FlattenTree(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Path == ".gts/index.json" {
			found = true
			break
		}
	}
	if !found {
		paths := make([]string, len(entries))
		for i, e := range entries {
			paths[i] = e.Path
		}
		t.Fatalf("expected .gts/index.json in tree, got: %v", paths)
	}
}

func TestBuildTree_NoSidecarDir_OK(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatal(err)
	}
	stg, _ := r.ReadStaging()
	_, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree should succeed without sidecar dir: %v", err)
	}
}

func TestBuildTree_SidecarNestedFiles(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatal(err)
	}
	// Nested sidecar files
	os.MkdirAll(filepath.Join(r.RootDir, ".gts", "sub"), 0o755)
	os.WriteFile(filepath.Join(r.RootDir, ".gts", "index.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(r.RootDir, ".gts", "sub", "data.json"), []byte("[]"), 0o644)

	stg, _ := r.ReadStaging()
	treeHash, _ := r.BuildTree(stg)
	entries, _ := r.FlattenTree(treeHash)

	want := map[string]bool{".gts/index.json": false, ".gts/sub/data.json": false}
	for _, e := range entries {
		if _, ok := want[e.Path]; ok {
			want[e.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing sidecar file in tree: %s", path)
		}
	}
}

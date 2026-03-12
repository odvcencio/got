package coord

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoModDeps(t *testing.T) {
	dir := t.TempDir()
	gomod := `module github.com/odvcencio/orchard

go 1.25

require (
	github.com/odvcencio/graft v0.2.6
	github.com/odvcencio/gotreesitter v0.6.0
)

replace github.com/odvcencio/gotreesitter => /home/draco/work/gotreesitter
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := ParseGoModDeps(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("ParseGoModDeps: %v", err)
	}

	if deps.Module != "github.com/odvcencio/orchard" {
		t.Errorf("module = %q", deps.Module)
	}
	if len(deps.Requires) != 2 {
		t.Fatalf("expected 2 requires, got %d", len(deps.Requires))
	}
	if deps.Replaces["github.com/odvcencio/gotreesitter"] != "/home/draco/work/gotreesitter" {
		t.Errorf("replace = %q", deps.Replaces["github.com/odvcencio/gotreesitter"])
	}
}

func TestBuildWorkspaceGraph(t *testing.T) {
	// Create mock workspace dirs with go.mod files
	root := t.TempDir()

	graftDir := filepath.Join(root, "graft")
	os.MkdirAll(graftDir, 0o755)
	os.WriteFile(filepath.Join(graftDir, "go.mod"), []byte(`module github.com/odvcencio/graft
go 1.25
require github.com/odvcencio/gotreesitter v0.6.0
replace github.com/odvcencio/gotreesitter => `+filepath.Join(root, "gotreesitter")+`
`), 0o644)

	orchardDir := filepath.Join(root, "orchard")
	os.MkdirAll(orchardDir, 0o755)
	os.WriteFile(filepath.Join(orchardDir, "go.mod"), []byte(`module github.com/odvcencio/orchard
go 1.25
require github.com/odvcencio/graft v0.2.6
`), 0o644)

	gtsDir := filepath.Join(root, "gotreesitter")
	os.MkdirAll(gtsDir, 0o755)
	os.WriteFile(filepath.Join(gtsDir, "go.mod"), []byte(`module github.com/odvcencio/gotreesitter
go 1.24
`), 0o644)

	workspaces := map[string]string{
		"graft":        graftDir,
		"orchard":      orchardDir,
		"gotreesitter": gtsDir,
	}

	graph, err := BuildWorkspaceGraph(workspaces)
	if err != nil {
		t.Fatalf("BuildWorkspaceGraph: %v", err)
	}

	// orchard depends on graft
	deps := graph.DependentsOf("graft")
	found := false
	for _, d := range deps {
		if d == "orchard" {
			found = true
		}
	}
	if !found {
		t.Error("expected orchard to depend on graft")
	}

	// graft depends on gotreesitter
	deps2 := graph.DependentsOf("gotreesitter")
	found2 := false
	for _, d := range deps2 {
		if d == "graft" {
			found2 = true
		}
	}
	if !found2 {
		t.Error("expected graft to depend on gotreesitter")
	}
}

func TestAutoDiscoverWorkspaces(t *testing.T) {
	root := t.TempDir()

	// Create a repo with go.mod that has replace directives
	repoDir := filepath.Join(root, "myrepo")
	os.MkdirAll(repoDir, 0o755)

	siblingDir := filepath.Join(root, "sibling")
	os.MkdirAll(siblingDir, 0o755)
	os.WriteFile(filepath.Join(siblingDir, "go.mod"), []byte("module github.com/example/sibling\ngo 1.25\n"), 0o644)

	depDir := filepath.Join(root, "dep")
	os.MkdirAll(depDir, 0o755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/example/dep\ngo 1.25\n"), 0o644)

	gomod := fmt.Sprintf(`module github.com/example/myrepo
go 1.25
require github.com/example/dep v1.0.0
replace github.com/example/dep => %s
`, depDir)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte(gomod), 0o644)

	discovered, err := AutoDiscoverWorkspaces(repoDir)
	if err != nil {
		t.Fatalf("AutoDiscoverWorkspaces: %v", err)
	}

	// Should find dep (from replace) and sibling (from directory scan)
	if _, ok := discovered["dep"]; !ok {
		t.Error("expected dep from replace directive")
	}
	if _, ok := discovered["sibling"]; !ok {
		t.Error("expected sibling from directory scan")
	}
}

package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

// Test 1: Init creates .got/ structure (HEAD, objects/, refs/heads/).
func TestInit_CreatesStructure(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init(%q): %v", dir, err)
	}
	if r.RootDir != dir {
		t.Errorf("RootDir = %q, want %q", r.RootDir, dir)
	}

	gotDir := filepath.Join(dir, ".got")
	if r.GotDir != gotDir {
		t.Errorf("GotDir = %q, want %q", r.GotDir, gotDir)
	}

	// .got/ directory exists
	assertDir(t, gotDir)

	// HEAD file exists
	assertFile(t, filepath.Join(gotDir, "HEAD"))

	// objects/ directory exists
	assertDir(t, filepath.Join(gotDir, "objects"))

	// refs/heads/ directory exists
	assertDir(t, filepath.Join(gotDir, "refs", "heads"))
	assertDir(t, filepath.Join(gotDir, "logs", "refs", "heads"))

	// Store is non-nil
	if r.Store == nil {
		t.Error("Store is nil after Init")
	}
}

// Test 2: Init on existing repo returns error.
func TestInit_ExistingRepo_Error(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}

	_, err = Init(dir)
	if err == nil {
		t.Fatal("second Init should fail on existing repo, got nil error")
	}
}

// Test 3: Open finds .got/ from subdirectory.
func TestOpen_FromSubdirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	sub := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	r, err := Open(sub)
	if err != nil {
		t.Fatalf("Open(%q): %v", sub, err)
	}

	if r.RootDir != dir {
		t.Errorf("RootDir = %q, want %q", r.RootDir, dir)
	}
	if r.GotDir != filepath.Join(dir, ".got") {
		t.Errorf("GotDir = %q, want %q", r.GotDir, filepath.Join(dir, ".got"))
	}
	if r.Store == nil {
		t.Error("Store is nil after Open")
	}
}

// Test 4: Open in non-repo directory returns error.
func TestOpen_NoRepo_Error(t *testing.T) {
	dir := t.TempDir()

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open should fail in non-repo directory, got nil error")
	}
}

// Test 5: HEAD defaults to "ref: refs/heads/main".
func TestInit_HeadDefault(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	ref, err := r.Head()
	if err != nil {
		t.Fatalf("Head(): %v", err)
	}
	if ref != "refs/heads/main" {
		t.Errorf("Head() = %q, want %q", ref, "refs/heads/main")
	}
}

// Test 6: UpdateRef + ResolveRef round-trip.
func TestUpdateRef_ResolveRef_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef = %q, want %q", got, h)
	}
}

// Test 7: ResolveRef with HEAD pointing to a branch that has a hash.
func TestResolveRef_HEAD_FollowsBranch(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// HEAD points to refs/heads/main by default, so write hash to that ref.
	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef(HEAD) = %q, want %q", got, h)
	}
}

// Test 8: ResolveRef short name (e.g., "main" resolves via refs/heads/main).
func TestResolveRef_ShortName(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef(main) = %q, want %q", got, h)
	}
}

// helpers

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected directory %q to exist: %v", path, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", path)
	}
}

func assertFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected file %q to exist: %v", path, err)
		return
	}
	if info.IsDir() {
		t.Errorf("%q exists but is a directory, expected file", path)
	}
}

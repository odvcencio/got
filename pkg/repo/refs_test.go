package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// ---------------------------------------------------------------------------
// ResolveRef
// ---------------------------------------------------------------------------

func TestResolveRef_HEAD_SymbolicToUnborn(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// HEAD points at refs/heads/main, but main doesn't exist yet.
	_, err = r.ResolveRef("HEAD")
	if err == nil {
		t.Fatal("expected error resolving HEAD on fresh repo, got nil")
	}
}

func TestResolveRef_HEAD_AfterCommit(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	resolved, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveRef(HEAD) = %q, want %q", resolved, h)
	}
}

func TestResolveRef_BranchRef(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// HEAD is symbolic to refs/heads/main, so resolving refs/heads/main
	// should give us the commit hash.
	resolved, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef(refs/heads/main): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveRef(refs/heads/main) = %q, want %q", resolved, h)
	}
}

func TestResolveRef_TagRef(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0", h, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	resolved, err := r.ResolveRef("refs/tags/v1.0")
	if err != nil {
		t.Fatalf("ResolveRef(refs/tags/v1.0): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveRef(refs/tags/v1.0) = %q, want %q", resolved, h)
	}
}

func TestResolveRef_MissingRefReturnsError(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.ResolveRef("refs/heads/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ref, got nil")
	}
}

func TestResolveRef_ShortBranchName(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// ResolveRef with a short name (not starting with "refs/") tries
	// refs/heads/<name>.
	resolved, err := r.ResolveRef("main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveRef(main) = %q, want %q", resolved, h)
	}
}

// ---------------------------------------------------------------------------
// UpdateRef
// ---------------------------------------------------------------------------

func TestUpdateRef_CreateNewRef(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash := object.Hash("aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666")
	if err := r.UpdateRef("refs/heads/feature", hash); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != hash {
		t.Fatalf("got %q, want %q", got, hash)
	}
}

func TestUpdateRef_OverwriteExistingRef(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash1 := object.Hash("aaaa1111")
	hash2 := object.Hash("bbbb2222")

	if err := r.UpdateRef("refs/heads/feature", hash1); err != nil {
		t.Fatalf("UpdateRef(1): %v", err)
	}
	if err := r.UpdateRef("refs/heads/feature", hash2); err != nil {
		t.Fatalf("UpdateRef(2): %v", err)
	}

	got, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != hash2 {
		t.Fatalf("got %q, want %q", got, hash2)
	}
}

func TestUpdateRef_CreatesParentDirectories(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash := object.Hash("dddd4444")
	if err := r.UpdateRef("refs/tags/release/v1.0", hash); err != nil {
		t.Fatalf("UpdateRef nested: %v", err)
	}

	got, err := r.ResolveRef("refs/tags/release/v1.0")
	if err != nil {
		t.Fatalf("ResolveRef nested: %v", err)
	}
	if got != hash {
		t.Fatalf("got %q, want %q", got, hash)
	}
}

// ---------------------------------------------------------------------------
// UpdateRefCAS
// ---------------------------------------------------------------------------

func TestUpdateRefCAS_MatchingOldSucceeds(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash1 := object.Hash("aaaa1111")
	hash2 := object.Hash("bbbb2222")

	if err := r.UpdateRef("refs/heads/feature", hash1); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	// CAS with correct old hash should succeed.
	if err := r.UpdateRefCAS("refs/heads/feature", hash2, hash1); err != nil {
		t.Fatalf("UpdateRefCAS: %v", err)
	}

	got, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != hash2 {
		t.Fatalf("got %q, want %q", got, hash2)
	}
}

func TestUpdateRefCAS_MismatchFails(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash1 := object.Hash("aaaa1111")
	hash2 := object.Hash("bbbb2222")
	wrongOld := object.Hash("cccc3333")

	if err := r.UpdateRef("refs/heads/feature", hash1); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	err = r.UpdateRefCAS("refs/heads/feature", hash2, wrongOld)
	if err == nil {
		t.Fatal("expected CAS mismatch error, got nil")
	}

	// Ref should remain at old value.
	got, err2 := r.ResolveRef("refs/heads/feature")
	if err2 != nil {
		t.Fatalf("ResolveRef: %v", err2)
	}
	if got != hash1 {
		t.Fatalf("ref changed despite CAS mismatch: got %q, want %q", got, hash1)
	}
}

// ---------------------------------------------------------------------------
// ListRefs
// ---------------------------------------------------------------------------

func TestListRefs_AllRefs(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create a tag and another branch.
	if err := r.CreateTag("v1.0", h, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := r.CreateBranch("feature", h); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	refs, err := r.ListRefs("")
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}

	// Expect heads/main, heads/feature, tags/v1.0.
	expectedRefs := []string{"heads/main", "heads/feature", "tags/v1.0"}
	for _, want := range expectedRefs {
		if _, ok := refs[want]; !ok {
			t.Errorf("missing ref %q in listing: %v", want, refs)
		}
	}
}

func TestListRefs_WithPrefix(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0", h, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := r.CreateBranch("feature", h); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Only heads.
	heads, err := r.ListRefs("heads")
	if err != nil {
		t.Fatalf("ListRefs(heads): %v", err)
	}
	for name := range heads {
		if !hasPrefix(name, "heads/") {
			t.Errorf("unexpected ref %q under heads prefix", name)
		}
	}
	if _, ok := heads["heads/main"]; !ok {
		t.Errorf("missing heads/main")
	}
	if _, ok := heads["heads/feature"]; !ok {
		t.Errorf("missing heads/feature")
	}

	// Only tags.
	tags, err := r.ListRefs("tags")
	if err != nil {
		t.Fatalf("ListRefs(tags): %v", err)
	}
	if _, ok := tags["tags/v1.0"]; !ok {
		t.Errorf("missing tags/v1.0")
	}
	if len(tags) != 1 {
		t.Errorf("expected 1 tag ref, got %d: %v", len(tags), tags)
	}
}

func TestListRefs_EmptyRepo(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	refs, err := r.ListRefs("")
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}

	// Fresh repo has no branch files (HEAD is symbolic but refs/heads/main
	// doesn't exist yet), so we expect an empty map.
	if len(refs) != 0 {
		t.Errorf("expected 0 refs, got %d: %v", len(refs), refs)
	}
}

// ---------------------------------------------------------------------------
// Head
// ---------------------------------------------------------------------------

func TestHead_SymbolicRef(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "refs/heads/main" {
		t.Fatalf("Head = %q, want %q", head, "refs/heads/main")
	}
}

func TestHead_DetachedHash(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Write a raw hash into HEAD to simulate detached state.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(h)+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != string(h) {
		t.Fatalf("Head = %q, want %q", head, h)
	}

	// ResolveRef(HEAD) should return the hash directly.
	resolved, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveRef(HEAD) = %q, want %q", resolved, h)
	}
}

// ---------------------------------------------------------------------------
// refsBaseDir
// ---------------------------------------------------------------------------

func TestRefsBaseDir_DefaultIsGraftDir(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	got := r.refsBaseDir()
	if got != r.GraftDir {
		t.Fatalf("refsBaseDir = %q, want GraftDir %q", got, r.GraftDir)
	}
}

func TestRefsBaseDir_ReturnsCommonDirWhenSet(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	r.CommonDir = "/some/shared/dir"
	got := r.refsBaseDir()
	if got != "/some/shared/dir" {
		t.Fatalf("refsBaseDir = %q, want CommonDir %q", got, "/some/shared/dir")
	}
}

// ---------------------------------------------------------------------------
// ResolveTreeish
// ---------------------------------------------------------------------------

func TestResolveTreeish_BranchName(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	resolved, err := r.ResolveTreeish("main")
	if err != nil {
		t.Fatalf("ResolveTreeish(main): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveTreeish(main) = %q, want %q", resolved, h)
	}
}

func TestResolveTreeish_TagName(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0", h, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	resolved, err := r.ResolveTreeish("v1.0")
	if err != nil {
		t.Fatalf("ResolveTreeish(v1.0): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveTreeish(v1.0) = %q, want %q", resolved, h)
	}
}

func TestResolveTreeish_HEAD(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	resolved, err := r.ResolveTreeish("HEAD")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveTreeish(HEAD) = %q, want %q", resolved, h)
	}
}

func TestResolveTreeish_RawHash(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Passing the raw commit hash should work.
	resolved, err := r.ResolveTreeish(string(h))
	if err != nil {
		t.Fatalf("ResolveTreeish(raw hash): %v", err)
	}
	if resolved != h {
		t.Fatalf("ResolveTreeish(raw hash) = %q, want %q", resolved, h)
	}
}

func TestResolveTreeish_UnknownReturnsError(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.ResolveTreeish("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown treeish, got nil")
	}
}

func TestResolveTreeish_TagTakesPriorityOverBranch(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h1, err := r.Commit("first", "test-author")
	if err != nil {
		t.Fatalf("Commit(first): %v", err)
	}

	// Make a second commit so we have two distinct hashes.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 2 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h2, err := r.Commit("second", "test-author")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Create a tag named "ambiguous" pointing at h1.
	if err := r.CreateTag("ambiguous", h1, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	// Create a branch named "ambiguous" pointing at h2.
	if err := r.CreateBranch("ambiguous", h2); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// ResolveTreeish tries tag first, then branch.
	resolved, err := r.ResolveTreeish("ambiguous")
	if err != nil {
		t.Fatalf("ResolveTreeish(ambiguous): %v", err)
	}
	if resolved != h1 {
		t.Fatalf("ResolveTreeish(ambiguous) = %q, want tag hash %q", resolved, h1)
	}
}

// hasPrefix is a test helper.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

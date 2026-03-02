package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// helper: create a repo with an initial commit so branches can be resolved.
func setupRepoWithCommit(t *testing.T) (*Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a file and commit it.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial commit", "test <test@test.com>"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return r, dir
}

// Test 1: Create a worktree, list it, verify it appears alongside main.
func TestWorktree_AddAndList(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	// Create a branch for the worktree.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Add a worktree.
	wtPath := filepath.Join(dir, "wt-feature")
	wtRepo, err := r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Verify the worktree repo fields.
	if wtRepo.RootDir != wtPath {
		t.Errorf("wtRepo.RootDir = %q, want %q", wtRepo.RootDir, wtPath)
	}
	if wtRepo.CommonDir == "" {
		t.Error("wtRepo.CommonDir is empty, expected it to be set")
	}
	if !wtRepo.IsLinkedWorktree() {
		t.Error("wtRepo.IsLinkedWorktree() = false, want true")
	}
	if r.IsLinkedWorktree() {
		t.Error("main repo IsLinkedWorktree() = true, want false")
	}

	// Verify .graft file exists in the worktree (not a directory).
	graftPath := filepath.Join(wtPath, ".graft")
	info, err := os.Stat(graftPath)
	if err != nil {
		t.Fatalf("stat .graft file: %v", err)
	}
	if info.IsDir() {
		t.Error(".graft in worktree should be a file, not directory")
	}

	// Verify hello.txt was checked out in the worktree.
	content, err := os.ReadFile(filepath.Join(wtPath, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt in worktree: %v", err)
	}
	if string(content) != "hello world\n" {
		t.Errorf("hello.txt content = %q, want %q", string(content), "hello world\n")
	}

	// List worktrees.
	infos, err := r.WorktreeList()
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("WorktreeList returned %d entries, want 2", len(infos))
	}

	// First entry is the main worktree.
	if infos[0].Branch != "main" {
		t.Errorf("main worktree branch = %q, want %q", infos[0].Branch, "main")
	}
	if infos[0].Path != dir {
		t.Errorf("main worktree path = %q, want %q", infos[0].Path, dir)
	}

	// Second entry is the linked worktree.
	if infos[1].Name != "wt-feature" {
		t.Errorf("linked worktree name = %q, want %q", infos[1].Name, "wt-feature")
	}
	if infos[1].Branch != "feature" {
		t.Errorf("linked worktree branch = %q, want %q", infos[1].Branch, "feature")
	}
	if infos[1].Path != wtPath {
		t.Errorf("linked worktree path = %q, want %q", infos[1].Path, wtPath)
	}
	if infos[1].Head != headHash {
		t.Errorf("linked worktree head = %q, want %q", infos[1].Head, headHash)
	}
}

// Test 2: Worktree has independent HEAD from main repo.
func TestWorktree_SeparateHEAD(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-feature")
	wtRepo, err := r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Main repo HEAD points to main.
	mainBranch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("main CurrentBranch: %v", err)
	}
	if mainBranch != "main" {
		t.Errorf("main branch = %q, want %q", mainBranch, "main")
	}

	// Worktree HEAD points to feature.
	wtBranch, err := wtRepo.CurrentBranch()
	if err != nil {
		t.Fatalf("worktree CurrentBranch: %v", err)
	}
	if wtBranch != "feature" {
		t.Errorf("worktree branch = %q, want %q", wtBranch, "feature")
	}

	// They are independent: changing worktree HEAD doesn't affect main.
	// Create a new commit in the worktree.
	if err := os.WriteFile(filepath.Join(wtPath, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	if err := wtRepo.Add([]string{"feature.txt"}); err != nil {
		t.Fatalf("worktree Add: %v", err)
	}
	newHash, err := wtRepo.Commit("feature commit", "test <test@test.com>")
	if err != nil {
		t.Fatalf("worktree Commit: %v", err)
	}

	// Worktree HEAD should now be at the new commit.
	wtHead, err := wtRepo.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("worktree ResolveRef HEAD: %v", err)
	}
	if wtHead != newHash {
		t.Errorf("worktree HEAD = %q, want %q", wtHead, newHash)
	}

	// Main HEAD should still be at the original commit.
	mainHead, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("main ResolveRef HEAD: %v", err)
	}
	if mainHead != headHash {
		t.Errorf("main HEAD = %q, want %q (should not have changed)", mainHead, headHash)
	}
}

// Test 3: Commit in worktree creates object visible from main repo.
func TestWorktree_SharedObjects(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-feature")
	wtRepo, err := r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Create a commit in the worktree.
	if err := os.WriteFile(filepath.Join(wtPath, "shared.txt"), []byte("shared\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := wtRepo.Add([]string{"shared.txt"}); err != nil {
		t.Fatalf("worktree Add: %v", err)
	}
	commitHash, err := wtRepo.Commit("shared commit", "test <test@test.com>")
	if err != nil {
		t.Fatalf("worktree Commit: %v", err)
	}

	// The commit should be readable from the main repo's store.
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("main Store.ReadCommit(%s): %v", commitHash, err)
	}
	if commit.Message != "shared commit" {
		t.Errorf("commit message = %q, want %q", commit.Message, "shared commit")
	}

	// The feature branch ref should be updated (shared refs).
	featureHash, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("main ResolveRef feature: %v", err)
	}
	if featureHash != commitHash {
		t.Errorf("feature branch = %q, want %q", featureHash, commitHash)
	}
}

// Test 4: Remove cleans up both directories.
func TestWorktree_Remove(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-feature")
	_, err = r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Verify both directories exist before removal.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}
	wtMetaDir := filepath.Join(r.GraftDir, "worktrees", "wt-feature")
	if _, err := os.Stat(wtMetaDir); err != nil {
		t.Fatalf("worktree metadata dir should exist: %v", err)
	}

	// Remove the worktree.
	if err := r.WorktreeRemove("wt-feature"); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}

	// Both should be gone.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists after remove")
	}
	if _, err := os.Stat(wtMetaDir); !os.IsNotExist(err) {
		t.Errorf("worktree metadata dir still exists after remove")
	}

	// List should only show the main worktree.
	infos, err := r.WorktreeList()
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("WorktreeList returned %d entries, want 1", len(infos))
	}
}

// Test 5: Prune removes stale entries where path doesn't exist.
func TestWorktree_Prune(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("stale", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-stale")
	_, err = r.WorktreeAdd(wtPath, "stale")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Manually delete the worktree working directory (simulating external removal).
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("RemoveAll worktree: %v", err)
	}

	// The metadata entry still exists.
	wtMetaDir := filepath.Join(r.GraftDir, "worktrees", "wt-stale")
	if _, err := os.Stat(wtMetaDir); err != nil {
		t.Fatalf("metadata should exist before prune: %v", err)
	}

	// Prune.
	if err := r.WorktreePrune(); err != nil {
		t.Fatalf("WorktreePrune: %v", err)
	}

	// The metadata entry should be gone.
	if _, err := os.Stat(wtMetaDir); !os.IsNotExist(err) {
		t.Error("metadata dir should be removed after prune")
	}

	// List should only show the main worktree.
	infos, err := r.WorktreeList()
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("WorktreeList returned %d entries after prune, want 1", len(infos))
	}
}

// Test 6: Open recognizes a linked worktree.
func TestWorktree_Open(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-feature")
	_, err = r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Open from the worktree path.
	opened, err := Open(wtPath)
	if err != nil {
		t.Fatalf("Open(%q): %v", wtPath, err)
	}

	if !opened.IsLinkedWorktree() {
		t.Error("opened repo should be a linked worktree")
	}
	if opened.RootDir != wtPath {
		t.Errorf("RootDir = %q, want %q", opened.RootDir, wtPath)
	}
	if opened.CommonDir == "" {
		t.Error("CommonDir is empty")
	}

	// Should be able to resolve refs.
	h, err := opened.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef feature: %v", err)
	}
	if h != headHash {
		t.Errorf("feature hash = %q, want %q", h, headHash)
	}

	// HEAD should point to the feature branch.
	branch, err := opened.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("branch = %q, want %q", branch, "feature")
	}
}

// Test 7: Cannot create worktree from a linked worktree.
func TestWorktree_CannotNest(t *testing.T) {
	r, dir := setupRepoWithCommit(t)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.CreateBranch("other", headHash); err != nil {
		t.Fatalf("CreateBranch other: %v", err)
	}

	wtPath := filepath.Join(dir, "wt-feature")
	wtRepo, err := r.WorktreeAdd(wtPath, "feature")
	if err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Try to add another worktree from the linked worktree.
	_, err = wtRepo.WorktreeAdd(filepath.Join(dir, "wt-nested"), "other")
	if err == nil {
		t.Fatal("WorktreeAdd from linked worktree should fail, got nil error")
	}
}

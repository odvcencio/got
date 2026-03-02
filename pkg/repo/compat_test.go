package repo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCompat_InitCreatesValidStructure verifies that Init produces all expected
// directories and files, and that HEAD contains the canonical default content.
func TestCompat_InitCreatesValidStructure(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	graftDir := filepath.Join(dir, ".graft")

	// Verify all expected directories exist.
	expectedDirs := []string{
		filepath.Join(graftDir, "objects"),
		filepath.Join(graftDir, "refs", "heads"),
		filepath.Join(graftDir, "logs", "refs", "heads"),
	}
	for _, d := range expectedDirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", d)
		}
	}

	// Verify HEAD file exists and contains the canonical content.
	headPath := filepath.Join(graftDir, "HEAD")
	headData, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	if string(headData) != "ref: refs/heads/main\n" {
		t.Errorf("HEAD content = %q, want %q", string(headData), "ref: refs/heads/main\n")
	}

	// Verify Repo fields are set correctly.
	if r.RootDir != dir {
		t.Errorf("RootDir = %q, want %q", r.RootDir, dir)
	}
	if r.GotDir != graftDir {
		t.Errorf("GotDir = %q, want %q", r.GotDir, graftDir)
	}
	if r.Store == nil {
		t.Error("Store is nil after Init")
	}
}

// TestCompat_CommitObjectRoundTrip creates files, adds, commits, then reads
// the commit object back and verifies its fields are well-formed: valid tree
// hash, valid author, non-empty message, correct parent count. It also reads
// the tree and verifies it contains the expected file entries.
func TestCompat_CommitObjectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files.
	files := map[string]string{
		"hello.txt": "hello world\n",
		"main.go":   "package main\n\nfunc main() {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Stage files.
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	if err := r.Add(paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// First commit.
	h1, err := r.Commit("initial commit", "Alice <alice@example.com>")
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if h1 == "" {
		t.Fatal("first Commit returned empty hash")
	}

	// Read back the commit object.
	c1, err := r.Store.ReadCommit(h1)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", h1, err)
	}

	// Verify commit fields.
	if c1.TreeHash == "" {
		t.Error("first commit: TreeHash is empty")
	}
	if c1.Author != "Alice <alice@example.com>" {
		t.Errorf("first commit: Author = %q, want %q", c1.Author, "Alice <alice@example.com>")
	}
	if c1.Message != "initial commit" {
		t.Errorf("first commit: Message = %q, want %q", c1.Message, "initial commit")
	}
	if c1.Timestamp == 0 {
		t.Error("first commit: Timestamp is zero")
	}
	if len(c1.Parents) != 0 {
		t.Errorf("first commit: expected 0 parents, got %d", len(c1.Parents))
	}

	// Read the tree and verify it contains expected file entries.
	treeEntries, err := r.FlattenTree(c1.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree(%s): %v", c1.TreeHash, err)
	}
	if len(treeEntries) != len(files) {
		t.Fatalf("tree has %d entries, want %d", len(treeEntries), len(files))
	}

	treePaths := make(map[string]bool)
	for _, entry := range treeEntries {
		treePaths[entry.Path] = true
		if entry.BlobHash == "" {
			t.Errorf("tree entry %q has empty BlobHash", entry.Path)
		}
	}
	for name := range files {
		if !treePaths[name] {
			t.Errorf("tree missing expected entry %q", name)
		}
	}

	// Second commit: modify a file.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world v2\n"), 0o644); err != nil {
		t.Fatalf("write hello.txt v2: %v", err)
	}
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add hello.txt: %v", err)
	}

	h2, err := r.Commit("second commit", "Alice <alice@example.com>")
	if err != nil {
		t.Fatalf("second Commit: %v", err)
	}

	c2, err := r.Store.ReadCommit(h2)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", h2, err)
	}

	if len(c2.Parents) != 1 {
		t.Fatalf("second commit: expected 1 parent, got %d", len(c2.Parents))
	}
	if c2.Parents[0] != h1 {
		t.Errorf("second commit: parent = %q, want %q", c2.Parents[0], h1)
	}
	if c2.TreeHash == "" {
		t.Error("second commit: TreeHash is empty")
	}
	if c2.TreeHash == c1.TreeHash {
		t.Error("second commit: TreeHash should differ from first (file was modified)")
	}
}

// TestCompat_BranchAndTagRefs creates commits, creates branches and tags,
// and verifies that refs resolve correctly.
func TestCompat_BranchAndTagRefs(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a file, add, and commit.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write file.txt: %v", err)
	}
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h1, err := r.Commit("first commit", "tester")
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}

	// Second commit.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content v2\n"), 0o644); err != nil {
		t.Fatalf("write file.txt v2: %v", err)
	}
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	h2, err := r.Commit("second commit", "tester")
	if err != nil {
		t.Fatalf("second Commit: %v", err)
	}

	// Create a branch "feature" pointing at h1.
	if err := r.CreateBranch("feature", h1); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	// Verify the branch resolves to h1.
	resolved, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}
	if resolved != h1 {
		t.Errorf("feature branch = %q, want %q", resolved, h1)
	}

	// Verify short name resolution works.
	resolvedShort, err := r.ResolveRef("feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature short): %v", err)
	}
	if resolvedShort != h1 {
		t.Errorf("feature branch (short) = %q, want %q", resolvedShort, h1)
	}

	// Create a second branch "develop" pointing at h2.
	if err := r.CreateBranch("develop", h2); err != nil {
		t.Fatalf("CreateBranch(develop): %v", err)
	}

	resolvedDev, err := r.ResolveRef("refs/heads/develop")
	if err != nil {
		t.Fatalf("ResolveRef(develop): %v", err)
	}
	if resolvedDev != h2 {
		t.Errorf("develop branch = %q, want %q", resolvedDev, h2)
	}

	// Create a lightweight tag "v1.0" pointing at h1.
	if err := r.CreateTag("v1.0", h1, false); err != nil {
		t.Fatalf("CreateTag(v1.0): %v", err)
	}

	resolvedTag, err := r.ResolveRef("refs/tags/v1.0")
	if err != nil {
		t.Fatalf("ResolveRef(v1.0): %v", err)
	}
	if resolvedTag != h1 {
		t.Errorf("tag v1.0 = %q, want %q", resolvedTag, h1)
	}

	// Verify HEAD still resolves to h2 (main branch).
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != h2 {
		t.Errorf("HEAD = %q, want %q", headHash, h2)
	}

	// Verify ListBranches returns all branches.
	branches, err := r.ListBranches()
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	branchSet := make(map[string]bool)
	for _, b := range branches {
		branchSet[b] = true
	}
	for _, expected := range []string{"main", "feature", "develop"} {
		if !branchSet[expected] {
			t.Errorf("ListBranches missing %q", expected)
		}
	}
}

// TestCompat_TreeFlattenRoundTrip writes a nested directory structure,
// commits, flattens the tree, and verifies all paths and blob contents.
func TestCompat_TreeFlattenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a nested directory structure.
	files := map[string]string{
		"a/b/c.txt": "deep nested content\n",
		"a/d.txt":   "mid level content\n",
		"e.txt":     "top level content\n",
	}
	for name, content := range files {
		absPath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("MkdirAll for %s: %v", name, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Stage and commit.
	addPaths := make([]string, 0, len(files))
	for name := range files {
		addPaths = append(addPaths, name)
	}
	sort.Strings(addPaths)
	if err := r.Add(addPaths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitHash, err := r.Commit("nested structure", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Read commit and flatten tree.
	c, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}

	entries, err := r.FlattenTree(c.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(entries) != len(files) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(files))
	}

	// Build a map from path -> entry for verification.
	entryMap := make(map[string]TreeFileEntry)
	for _, entry := range entries {
		entryMap[entry.Path] = entry
	}

	// Verify all expected paths are present and blob content matches.
	for name, expectedContent := range files {
		entry, ok := entryMap[name]
		if !ok {
			t.Errorf("missing path %q in flattened tree", name)
			continue
		}
		if entry.BlobHash == "" {
			t.Errorf("path %q has empty BlobHash", name)
			continue
		}

		// Read the blob back and verify content.
		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			t.Errorf("ReadBlob(%s) for %q: %v", entry.BlobHash, name, err)
			continue
		}
		if string(blob.Data) != expectedContent {
			t.Errorf("blob content for %q = %q, want %q", name, string(blob.Data), expectedContent)
		}
	}

	// Verify tree structure: entries under "a/" should be in subdirectories.
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "a/") || entry.Path == "e.txt" {
			// expected
		} else {
			t.Errorf("unexpected path in tree: %q", entry.Path)
		}
	}
}

// TestCompat_ObjectStoreIntegrity writes several commits with files, then
// runs Store.Verify() to confirm no integrity errors.
func TestCompat_ObjectStoreIntegrity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create multiple commits with various files.
	commitData := []struct {
		files   map[string]string
		message string
	}{
		{
			files:   map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n"},
			message: "first commit",
		},
		{
			files:   map[string]string{"a.txt": "alpha v2\n", "c.txt": "gamma\n"},
			message: "second commit",
		},
		{
			files:   map[string]string{"d/e.txt": "delta\n", "d/f.txt": "epsilon\n"},
			message: "third commit",
		},
	}

	for _, cd := range commitData {
		for name, content := range cd.files {
			absPath := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				t.Fatalf("MkdirAll for %s: %v", name, err)
			}
			if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}

		paths := make([]string, 0, len(cd.files))
		for name := range cd.files {
			paths = append(paths, name)
		}
		sort.Strings(paths)
		if err := r.Add(paths); err != nil {
			t.Fatalf("Add for %q: %v", cd.message, err)
		}

		if _, err := r.Commit(cd.message, "tester"); err != nil {
			t.Fatalf("Commit(%q): %v", cd.message, err)
		}
	}

	// Verify the object store integrity.
	report, err := r.Store.Verify()
	if err != nil {
		t.Fatalf("Store.Verify() returned error: %v", err)
	}
	if report == nil {
		t.Fatal("Store.Verify() returned nil report")
	}

	// There should be at least some loose objects (blobs, trees, commits).
	totalObjects := report.LooseObjects + report.PackObjects
	if totalObjects == 0 {
		t.Error("Store.Verify() found 0 total objects, expected some")
	}
}

// TestCompat_ReflogConsistency creates several commits, reads the reflog, and
// verifies entries have correct old/new hashes that form a chain: the NewHash
// of entry N-1 (chronologically) equals the OldHash of entry N.
func TestCompat_ReflogConsistency(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create several commits to generate reflog entries.
	commitHashes := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		content := []byte("version " + string(rune('A'+i)) + "\n")
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), content, 0o644); err != nil {
			t.Fatalf("write file.txt v%d: %v", i, err)
		}
		if err := r.Add([]string{"file.txt"}); err != nil {
			t.Fatalf("Add v%d: %v", i, err)
		}
		h, err := r.Commit("commit "+string(rune('A'+i)), "tester")
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
		commitHashes = append(commitHashes, string(h))
	}

	// Read the reflog for refs/heads/main.
	entries, err := r.ReadReflog("refs/heads/main", 0)
	if err != nil {
		t.Fatalf("ReadReflog: %v", err)
	}

	if len(entries) < 4 {
		t.Fatalf("reflog has %d entries, want at least 4", len(entries))
	}

	// ReadReflog returns newest first. Reverse to get chronological order.
	chronological := make([]ReflogEntry, len(entries))
	copy(chronological, entries)
	for i, j := 0, len(chronological)-1; i < j; i, j = i+1, j-1 {
		chronological[i], chronological[j] = chronological[j], chronological[i]
	}

	// Verify the chain: NewHash of entry i == OldHash of entry i+1.
	for i := 0; i < len(chronological)-1; i++ {
		if chronological[i].NewHash != chronological[i+1].OldHash {
			t.Errorf("reflog chain broken at entry %d: NewHash=%q, next OldHash=%q",
				i, chronological[i].NewHash, chronological[i+1].OldHash)
		}
	}

	// Verify the first entry's OldHash is the zero hash (initial commit).
	firstOld := string(chronological[0].OldHash)
	if firstOld != zeroHash {
		t.Errorf("first reflog entry OldHash = %q, want zero hash", firstOld)
	}

	// Verify the last entry's NewHash matches the latest commit.
	lastNew := string(chronological[len(chronological)-1].NewHash)
	if lastNew != commitHashes[len(commitHashes)-1] {
		t.Errorf("last reflog entry NewHash = %q, want %q", lastNew, commitHashes[len(commitHashes)-1])
	}

	// Verify each entry has a non-zero timestamp.
	for i, entry := range chronological {
		if entry.Timestamp == 0 {
			t.Errorf("reflog entry %d has zero timestamp", i)
		}
	}
}

// TestCompat_StagingRoundTrip adds files, reads staging, verifies all entries
// are present with valid blob hashes. Then modifies a file, re-adds, and
// verifies the blob hash changed.
func TestCompat_StagingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files.
	files := map[string]string{
		"x.txt":     "x content\n",
		"y.txt":     "y content\n",
		"sub/z.txt": "z content\n",
	}
	for name, content := range files {
		absPath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("MkdirAll for %s: %v", name, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Stage all files.
	addPaths := make([]string, 0, len(files))
	for name := range files {
		addPaths = append(addPaths, name)
	}
	sort.Strings(addPaths)
	if err := r.Add(addPaths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Read staging and verify.
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	if len(stg.Entries) != len(files) {
		t.Fatalf("staging has %d entries, want %d", len(stg.Entries), len(files))
	}

	for name := range files {
		entry, ok := stg.Entries[name]
		if !ok {
			t.Errorf("staging missing entry for %q", name)
			continue
		}
		if entry.BlobHash == "" {
			t.Errorf("staging entry %q has empty BlobHash", name)
		}
		if entry.Path != name {
			t.Errorf("staging entry path = %q, want %q", entry.Path, name)
		}
	}

	// Record the original blob hash for x.txt.
	originalBlobHash := stg.Entries["x.txt"].BlobHash

	// Modify x.txt and re-add.
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x content modified\n"), 0o644); err != nil {
		t.Fatalf("write modified x.txt: %v", err)
	}
	if err := r.Add([]string{"x.txt"}); err != nil {
		t.Fatalf("re-Add x.txt: %v", err)
	}

	// Read staging again and verify blob hash changed.
	stg2, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging after modify: %v", err)
	}

	newBlobHash := stg2.Entries["x.txt"].BlobHash
	if newBlobHash == "" {
		t.Fatal("modified x.txt has empty BlobHash")
	}
	if newBlobHash == originalBlobHash {
		t.Error("blob hash for x.txt did not change after modification")
	}

	// Verify unchanged files still have the same blob hash.
	if stg2.Entries["y.txt"].BlobHash != stg.Entries["y.txt"].BlobHash {
		t.Error("y.txt blob hash changed unexpectedly")
	}
	if stg2.Entries["sub/z.txt"].BlobHash != stg.Entries["sub/z.txt"].BlobHash {
		t.Error("sub/z.txt blob hash changed unexpectedly")
	}
}

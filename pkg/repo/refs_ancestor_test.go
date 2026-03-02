package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// ---------------------------------------------------------------------------
// parseRevisionSuffix (unit tests for the parser itself)
// ---------------------------------------------------------------------------

func TestParseRevisionSuffix_NoSuffix(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD")
	if base != "HEAD" {
		t.Fatalf("base = %q, want %q", base, "HEAD")
	}
	if len(ops) != 0 {
		t.Fatalf("ops = %v, want empty", ops)
	}
}

func TestParseRevisionSuffix_Tilde(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD~3")
	if base != "HEAD" {
		t.Fatalf("base = %q, want %q", base, "HEAD")
	}
	if len(ops) != 1 || !ops[0].tilde || ops[0].n != 3 {
		t.Fatalf("ops = %v, want [{tilde:true n:3}]", ops)
	}
}

func TestParseRevisionSuffix_BareTilde(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD~")
	if base != "HEAD" || len(ops) != 1 || !ops[0].tilde || ops[0].n != 1 {
		t.Fatalf("got base=%q ops=%v, want HEAD [{tilde:true n:1}]", base, ops)
	}
}

func TestParseRevisionSuffix_BareCaret(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD^")
	if base != "HEAD" || len(ops) != 1 || ops[0].tilde || ops[0].n != 1 {
		t.Fatalf("got base=%q ops=%v, want HEAD [{tilde:false n:1}]", base, ops)
	}
}

func TestParseRevisionSuffix_DoubleTilde(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD~~")
	if base != "HEAD" || len(ops) != 2 {
		t.Fatalf("got base=%q ops=%v", base, ops)
	}
	for i, op := range ops {
		if !op.tilde || op.n != 1 {
			t.Fatalf("ops[%d] = %+v, want {tilde:true n:1}", i, op)
		}
	}
}

func TestParseRevisionSuffix_DoubleCaret(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD^^")
	if base != "HEAD" || len(ops) != 2 {
		t.Fatalf("got base=%q ops=%v", base, ops)
	}
	for i, op := range ops {
		if op.tilde || op.n != 1 {
			t.Fatalf("ops[%d] = %+v, want {tilde:false n:1}", i, op)
		}
	}
}

func TestParseRevisionSuffix_Chained(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD~3^2")
	if base != "HEAD" || len(ops) != 2 {
		t.Fatalf("got base=%q ops=%v", base, ops)
	}
	if !ops[0].tilde || ops[0].n != 3 {
		t.Fatalf("ops[0] = %+v, want {tilde:true n:3}", ops[0])
	}
	if ops[1].tilde || ops[1].n != 2 {
		t.Fatalf("ops[1] = %+v, want {tilde:false n:2}", ops[1])
	}
}

func TestParseRevisionSuffix_AtAlias(t *testing.T) {
	base, ops := parseRevisionSuffix("@")
	if base != "HEAD" || len(ops) != 0 {
		t.Fatalf("got base=%q ops=%v, want HEAD []", base, ops)
	}

	base, ops = parseRevisionSuffix("@~2")
	if base != "HEAD" || len(ops) != 1 || !ops[0].tilde || ops[0].n != 2 {
		t.Fatalf("got base=%q ops=%v", base, ops)
	}
}

func TestParseRevisionSuffix_TildeZero(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD~0")
	if base != "HEAD" || len(ops) != 1 || !ops[0].tilde || ops[0].n != 0 {
		t.Fatalf("got base=%q ops=%v, want HEAD [{tilde:true n:0}]", base, ops)
	}
}

func TestParseRevisionSuffix_CaretZero(t *testing.T) {
	base, ops := parseRevisionSuffix("HEAD^0")
	if base != "HEAD" || len(ops) != 1 || ops[0].tilde || ops[0].n != 0 {
		t.Fatalf("got base=%q ops=%v, want HEAD [{tilde:false n:0}]", base, ops)
	}
}

// ---------------------------------------------------------------------------
// Helper: build a linear chain of N commits, returning hashes newest-first.
// ---------------------------------------------------------------------------

func buildLinearChain(t *testing.T, r *Repo, n int) []object.Hash {
	t.Helper()
	var hashes []object.Hash

	for i := 0; i < n; i++ {
		content := []byte("package main\n\nfunc main() { _ = " + strings.Repeat("x", i) + " }\n")
		if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), content, 0o644); err != nil {
			t.Fatalf("write main.go (iter %d): %v", i, err)
		}
		if err := r.Add([]string{"main.go"}); err != nil {
			t.Fatalf("Add (iter %d): %v", i, err)
		}
		h, err := r.Commit("commit "+strings.Repeat("x", i), "test-author")
		if err != nil {
			t.Fatalf("Commit (iter %d): %v", i, err)
		}
		hashes = append(hashes, h)
	}
	// Reverse so hashes[0] is the most recent (HEAD).
	for left, right := 0, len(hashes)-1; left < right; left, right = left+1, right-1 {
		hashes[left], hashes[right] = hashes[right], hashes[left]
	}
	return hashes
}

// ---------------------------------------------------------------------------
// Helper: build a merge commit.
// Creates a linear chain on main, then a divergent commit on a side branch,
// then merges them. Returns (mergeHash, mainParent, sideParent, baseHash).
// ---------------------------------------------------------------------------

func buildMergeCommit(t *testing.T) (*Repo, object.Hash, object.Hash, object.Hash) {
	t.Helper()
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Initial commit on main.
	baseHash, err := r.Commit("base", "test-author")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create a side branch at the base commit.
	if err := r.CreateBranch("side", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Make a second commit on main.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	mainParent, err := r.Commit("main-change", "test-author")
	if err != nil {
		t.Fatalf("Commit(main-change): %v", err)
	}

	// Switch to side branch and make a commit there.
	// We need to detach HEAD, write the side branch file, etc.
	// Simplest approach: directly create a commit on the side branch
	// by writing a commit object with the proper parent.
	sideTree, err := buildSideTree(t, r)
	if err != nil {
		t.Fatalf("buildSideTree: %v", err)
	}

	sideCommitObj := &object.CommitObj{
		TreeHash:  sideTree,
		Parents:   []object.Hash{baseHash},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "side-change",
	}
	sideParent, err := r.Store.WriteCommit(sideCommitObj)
	if err != nil {
		t.Fatalf("WriteCommit(side): %v", err)
	}
	if err := r.UpdateRef("refs/heads/side", sideParent); err != nil {
		t.Fatalf("UpdateRef(side): %v", err)
	}

	// Now create the merge commit with two parents: mainParent and sideParent.
	mergeCommitObj := &object.CommitObj{
		TreeHash:  mainParent, // reuse main tree hash for simplicity — not important for ref resolution tests
		Parents:   []object.Hash{mainParent, sideParent},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "merge side into main",
	}
	// Actually we need a valid tree hash. Let's read the mainParent commit's tree.
	mainCommit, err := r.Store.ReadCommit(mainParent)
	if err != nil {
		t.Fatalf("ReadCommit(mainParent): %v", err)
	}
	mergeCommitObj.TreeHash = mainCommit.TreeHash

	mergeHash, err := r.Store.WriteCommit(mergeCommitObj)
	if err != nil {
		t.Fatalf("WriteCommit(merge): %v", err)
	}

	// Update main branch to point at the merge commit.
	if err := r.UpdateRef("refs/heads/main", mergeHash); err != nil {
		t.Fatalf("UpdateRef(main->merge): %v", err)
	}
	// Update HEAD to point at the merge commit (HEAD -> refs/heads/main -> mergeHash).
	// HEAD is already symbolic to refs/heads/main, so updating the branch is sufficient.

	return r, mergeHash, mainParent, sideParent
}

func buildSideTree(t *testing.T, r *Repo) (object.Hash, error) {
	t.Helper()
	// Write a different blob for the side branch.
	sideBlob := &object.Blob{Data: []byte("package main\n\nfunc main() { _ = 2 }\n")}
	blobHash, err := r.Store.WriteBlob(sideBlob)
	if err != nil {
		return "", err
	}

	// Build a simple tree with just main.go.
	treeObj := &object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "main.go",
				Mode:     object.TreeModeFile,
				BlobHash: blobHash,
			},
		},
	}
	return r.Store.WriteTree(treeObj)
}

// ---------------------------------------------------------------------------
// ResolveTreeish with tilde notation
// ---------------------------------------------------------------------------

func TestResolveTreeish_TildeNotation(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	hashes := buildLinearChain(t, r, 5)
	// hashes[0]=HEAD, hashes[1]=HEAD~1, ..., hashes[4]=HEAD~4

	tests := []struct {
		spec string
		want object.Hash
	}{
		{"HEAD~1", hashes[1]},
		{"HEAD~2", hashes[2]},
		{"HEAD~3", hashes[3]},
		{"HEAD~4", hashes[4]},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := r.ResolveTreeish(tc.spec)
			if err != nil {
				t.Fatalf("ResolveTreeish(%q): %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveTreeish(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ResolveTreeish with caret notation
// ---------------------------------------------------------------------------

func TestResolveTreeish_CaretNotation(t *testing.T) {
	r, mergeHash, mainParent, sideParent := buildMergeCommit(t)

	tests := []struct {
		spec string
		want object.Hash
	}{
		{"HEAD^1", mainParent},
		{"HEAD^2", sideParent},
	}

	// HEAD currently points to the merge commit.
	headHash, err := r.ResolveTreeish("HEAD")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD): %v", err)
	}
	if headHash != mergeHash {
		t.Fatalf("HEAD = %q, want merge hash %q", headHash, mergeHash)
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := r.ResolveTreeish(tc.spec)
			if err != nil {
				t.Fatalf("ResolveTreeish(%q): %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveTreeish(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Chained notation: HEAD~2^2
// ---------------------------------------------------------------------------

func TestResolveTreeish_ChainedNotation(t *testing.T) {
	// Build a repo where HEAD~1 is a merge commit. We need:
	// HEAD -> commit C (normal)
	// HEAD~1 -> commit M (merge with 2 parents: P1, P2)
	// HEAD~1^2 -> P2

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Create base commit.
	baseHash, err := r.Commit("base", "test-author")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create a side commit directly in the store.
	sideTree, err := buildSideTree(t, r)
	if err != nil {
		t.Fatalf("buildSideTree: %v", err)
	}
	sideCommit := &object.CommitObj{
		TreeHash:  sideTree,
		Parents:   []object.Hash{baseHash},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "side",
	}
	sideHash, err := r.Store.WriteCommit(sideCommit)
	if err != nil {
		t.Fatalf("WriteCommit(side): %v", err)
	}

	// Make another commit on main (P1 of the merge).
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	mainP1, err := r.Commit("main-p1", "test-author")
	if err != nil {
		t.Fatalf("Commit(main-p1): %v", err)
	}

	// Read tree hash from mainP1 for the merge commit.
	mainP1Commit, err := r.Store.ReadCommit(mainP1)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}

	// Create merge commit M with parents [mainP1, sideHash].
	mergeObj := &object.CommitObj{
		TreeHash:  mainP1Commit.TreeHash,
		Parents:   []object.Hash{mainP1, sideHash},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "merge",
	}
	mergeHash, err := r.Store.WriteCommit(mergeObj)
	if err != nil {
		t.Fatalf("WriteCommit(merge): %v", err)
	}

	// Update main to the merge commit.
	if err := r.UpdateRef("refs/heads/main", mergeHash); err != nil {
		t.Fatalf("UpdateRef(main->merge): %v", err)
	}

	// Now make one more commit on top (this will be HEAD).
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 3 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	headHash, err := r.Commit("head", "test-author")
	if err != nil {
		t.Fatalf("Commit(head): %v", err)
	}

	// HEAD -> headHash, HEAD~1 -> mergeHash, HEAD~1^2 -> sideHash
	got, err := r.ResolveTreeish("HEAD~1^2")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD~1^2): %v", err)
	}
	if got != sideHash {
		t.Fatalf("ResolveTreeish(HEAD~1^2) = %q, want sideHash %q", got, sideHash)
	}

	// Sanity check HEAD~1 == mergeHash.
	gotMerge, err := r.ResolveTreeish("HEAD~1")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD~1): %v", err)
	}
	if gotMerge != mergeHash {
		t.Fatalf("HEAD~1 = %q, want %q", gotMerge, mergeHash)
	}

	_ = headHash // used implicitly via HEAD
}

// ---------------------------------------------------------------------------
// Bare ^ and ~ (no number = 1)
// ---------------------------------------------------------------------------

func TestResolveTreeish_BareCaretAndTilde(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	hashes := buildLinearChain(t, r, 3)

	// HEAD~ should be HEAD~1.
	got, err := r.ResolveTreeish("HEAD~")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD~): %v", err)
	}
	if got != hashes[1] {
		t.Fatalf("HEAD~ = %q, want %q", got, hashes[1])
	}

	// HEAD^ should be HEAD^1 (first parent, same as ~1 for non-merge).
	got, err = r.ResolveTreeish("HEAD^")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD^): %v", err)
	}
	if got != hashes[1] {
		t.Fatalf("HEAD^ = %q, want %q", got, hashes[1])
	}

	// HEAD~~ should be HEAD~2.
	got, err = r.ResolveTreeish("HEAD~~")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD~~): %v", err)
	}
	if got != hashes[2] {
		t.Fatalf("HEAD~~ = %q, want %q", got, hashes[2])
	}

	// HEAD^^ should be HEAD~2 (two first-parent steps).
	got, err = r.ResolveTreeish("HEAD^^")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD^^): %v", err)
	}
	if got != hashes[2] {
		t.Fatalf("HEAD^^ = %q, want %q", got, hashes[2])
	}
}

// ---------------------------------------------------------------------------
// @ alias for HEAD
// ---------------------------------------------------------------------------

func TestResolveTreeish_AtAlias(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	hashes := buildLinearChain(t, r, 3)

	// @ should resolve the same as HEAD.
	got, err := r.ResolveTreeish("@")
	if err != nil {
		t.Fatalf("ResolveTreeish(@): %v", err)
	}
	if got != hashes[0] {
		t.Fatalf("@ = %q, want %q", got, hashes[0])
	}

	// @~1 should be HEAD~1.
	got, err = r.ResolveTreeish("@~1")
	if err != nil {
		t.Fatalf("ResolveTreeish(@~1): %v", err)
	}
	if got != hashes[1] {
		t.Fatalf("@~1 = %q, want %q", got, hashes[1])
	}

	// @^1 should be HEAD^1.
	got, err = r.ResolveTreeish("@^1")
	if err != nil {
		t.Fatalf("ResolveTreeish(@^1): %v", err)
	}
	if got != hashes[1] {
		t.Fatalf("@^1 = %q, want %q", got, hashes[1])
	}
}

// ---------------------------------------------------------------------------
// ~0 and ^0 = the commit itself
// ---------------------------------------------------------------------------

func TestResolveTreeish_TildeZero(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := r.ResolveTreeish("HEAD~0")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD~0): %v", err)
	}
	if got != h {
		t.Fatalf("HEAD~0 = %q, want %q", got, h)
	}
}

func TestResolveTreeish_CaretZero(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := r.ResolveTreeish("HEAD^0")
	if err != nil {
		t.Fatalf("ResolveTreeish(HEAD^0): %v", err)
	}
	if got != h {
		t.Fatalf("HEAD^0 = %q, want %q", got, h)
	}
}

// ---------------------------------------------------------------------------
// Out of range errors
// ---------------------------------------------------------------------------

func TestResolveTreeish_OutOfRange(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	_ = buildLinearChain(t, r, 5)

	// HEAD~100 on a 5-commit chain (buildLinearChain creates 5 more after
	// initRepoWithFile, so total is 6 commits: chain of 5 + initial).
	// But ~100 is way past the root.
	_, err := r.ResolveTreeish("HEAD~100")
	if err == nil {
		t.Fatal("expected error for HEAD~100 on short chain, got nil")
	}
	if !strings.Contains(err.Error(), "no parents") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveTreeish_CaretOutOfRange(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	_ = buildLinearChain(t, r, 2)

	// HEAD^3 on a non-merge commit (only 1 parent).
	_, err := r.ResolveTreeish("HEAD^3")
	if err == nil {
		t.Fatal("expected error for HEAD^3 on non-merge commit, got nil")
	}
	if !strings.Contains(err.Error(), "requested parent 3") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Branch name with suffix
// ---------------------------------------------------------------------------

func TestResolveTreeish_BranchWithTilde(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	hashes := buildLinearChain(t, r, 3)

	got, err := r.ResolveTreeish("main~1")
	if err != nil {
		t.Fatalf("ResolveTreeish(main~1): %v", err)
	}
	if got != hashes[1] {
		t.Fatalf("main~1 = %q, want %q", got, hashes[1])
	}
}

// ---------------------------------------------------------------------------
// Raw hash with suffix
// ---------------------------------------------------------------------------

func TestResolveTreeish_RawHashWithTilde(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	hashes := buildLinearChain(t, r, 3)

	// Use the raw hash of HEAD with ~1.
	spec := string(hashes[0]) + "~1"
	got, err := r.ResolveTreeish(spec)
	if err != nil {
		t.Fatalf("ResolveTreeish(%q): %v", spec, err)
	}
	if got != hashes[1] {
		t.Fatalf("%s = %q, want %q", spec, got, hashes[1])
	}
}

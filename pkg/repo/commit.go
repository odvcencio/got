package repo

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// Commit creates a new commit from the current staging area.
//
//  1. Read staging
//  2. BuildTree from staging
//  3. Resolve HEAD to get parent commit hash (if any)
//  4. Create CommitObj with tree hash, parent, author, current timestamp, message
//  5. Write commit to store
//  6. Update current branch ref to new commit hash
//  7. Return commit hash
func (r *Repo) Commit(message, author string) (object.Hash, error) {
	// 1. Read staging.
	stg, err := r.ReadStaging()
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	if len(stg.Entries) == 0 {
		return "", fmt.Errorf("commit: nothing staged")
	}

	// 2. Build tree from staging.
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// 3. Resolve HEAD to get parent (may not exist for first commit).
	var parents []object.Hash
	parentHash, err := r.ResolveRef("HEAD")
	if err == nil && parentHash != "" {
		parents = append(parents, parentHash)
	}
	// If HEAD resolution fails (e.g., first commit, no ref file), that's fine.

	// 4. Create CommitObj.
	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}

	// 5. Write commit to store.
	commitHash, err := r.Store.WriteCommit(commitObj)
	if err != nil {
		return "", fmt.Errorf("commit: write commit: %w", err)
	}

	// 6. Update current branch ref.
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("commit: read HEAD: %w", err)
	}

	// head is either a ref path ("refs/heads/main") or a detached hash.
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRef(head, commitHash); err != nil {
			return "", fmt.Errorf("commit: update ref %q: %w", head, err)
		}
	} else {
		// Detached HEAD: write the hash directly to HEAD.
		headPath := fmt.Sprintf("%s/HEAD", r.GotDir)
		if err := os.WriteFile(headPath, []byte(string(commitHash)+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("commit: update HEAD: %w", err)
		}
	}

	// 7. Return commit hash.
	return commitHash, nil
}

// Log walks the commit history starting from the given hash, following
// first-parent links, returning up to limit commits in reverse-chronological
// order (newest first).
func (r *Repo) Log(start object.Hash, limit int) ([]*object.CommitObj, error) {
	var commits []*object.CommitObj
	current := start

	for len(commits) < limit {
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			// If we can't read the commit (e.g., doesn't exist), stop.
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return nil, fmt.Errorf("log: read commit %s: %w", current, err)
		}
		commits = append(commits, c)

		// Follow first parent.
		if len(c.Parents) == 0 {
			break
		}
		current = c.Parents[0]
	}

	return commits, nil
}

package repo

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// CommitSigner signs canonical commit payload bytes and returns an encoded
// signature string to be persisted in CommitObj.Signature.
type CommitSigner func(payload []byte) (string, error)

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
	return r.CommitWithSigner(message, author, nil)
}

// CommitWithSigner creates a new commit and signs it when signer is provided.
func (r *Repo) CommitWithSigner(message, author string, signer CommitSigner) (object.Hash, error) {
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
	if signer != nil {
		payload := object.CommitSigningPayload(commitObj)
		signature, err := signer(payload)
		if err != nil {
			return "", fmt.Errorf("commit: sign commit: %w", err)
		}
		commitObj.Signature = signature
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
		var updateErr error
		if parentHash == "" {
			updateErr = r.UpdateRefCAS(head, commitHash)
		} else {
			updateErr = r.UpdateRefCAS(head, commitHash, parentHash)
		}
		if updateErr != nil {
			return "", fmt.Errorf("commit: update ref %q: %w", head, updateErr)
		}
	} else {
		// Detached HEAD: update HEAD directly with a CAS against the old hash.
		if err := r.UpdateRefCAS("HEAD", commitHash, object.Hash(strings.TrimSpace(head))); err != nil {
			return "", fmt.Errorf("commit: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

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

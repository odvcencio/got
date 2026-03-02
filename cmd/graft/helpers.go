package main

import (
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

// shortHash delegates to repo.ShortHash for display purposes.
func shortHash(h object.Hash) string {
	return repo.ShortHash(h)
}

// branchName returns the current branch name (without refs/heads/ prefix),
// or "HEAD" if the repo is in detached HEAD state.
func branchName(r *repo.Repo) string {
	head, err := r.Head()
	if err == nil && strings.HasPrefix(head, "refs/heads/") {
		return strings.TrimPrefix(head, "refs/heads/")
	}
	return "HEAD"
}

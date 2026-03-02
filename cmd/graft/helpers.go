package main

import (
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

// shortHash returns the first 8 characters of a hash for display purposes.
func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) > 8 {
		return s[:8]
	}
	return s
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

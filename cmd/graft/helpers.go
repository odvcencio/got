package main

import "github.com/odvcencio/graft/pkg/object"

// shortHash returns the first 8 characters of a hash for display purposes.
func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

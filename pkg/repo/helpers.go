package repo

import (
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ShortHash returns the first 8 characters of a hash for display purposes.
func ShortHash(h object.Hash) string {
	s := string(h)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// shortHash is an unexported alias for ShortHash, kept for internal callers.
func shortHash(h object.Hash) string { return ShortHash(h) }

// commitTitle returns the first line of s (up to the first newline, or all of
// s if there is no newline).
func commitTitle(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

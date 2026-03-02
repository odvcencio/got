package repo

import (
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// shortHash returns the first 8 characters of a hash for display purposes.
func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// commitTitle returns the first line of s (up to the first newline, or all of
// s if there is no newline).
func commitTitle(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

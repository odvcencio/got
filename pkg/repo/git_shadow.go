package repo

import (
	"os"
	"path/filepath"
)

const shadowFailuresLog = "shadow-failures.log"

// HasShadowFailures reports whether the shadow-failures.log file exists and
// is non-empty, indicating that one or more git shadow operations failed.
func (r *Repo) HasShadowFailures() bool {
	info, err := os.Stat(filepath.Join(r.GraftDir, shadowFailuresLog))
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// ClearShadowFailures removes the shadow-failures.log file.
func (r *Repo) ClearShadowFailures() {
	os.Remove(filepath.Join(r.GraftDir, shadowFailuresLog))
}

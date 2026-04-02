package repo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// isSidecarPath returns true if the given forward-slash path belongs to one of
// the known sidecar directories (e.g. ".gts/index.json").
func isSidecarPath(p string) bool {
	for _, sidecar := range DefaultSidecarDirs {
		if strings.HasPrefix(p, sidecar+"/") {
			return true
		}
	}
	return false
}

// restoreSidecarsFromTree writes sidecar directory contents from a committed
// tree back to the working directory. Called after checkout to restore
// analysis artifacts (.gts/) that were committed via sidecar injection.
func (r *Repo) restoreSidecarsFromTree(treeHash object.Hash) {
	if treeHash == "" {
		return
	}

	// Clean stale sidecar files before restoring from new tree.
	for _, sidecar := range DefaultSidecarDirs {
		sidecarPath := filepath.Join(r.RootDir, sidecar)
		os.RemoveAll(sidecarPath)
	}

	entries, err := r.FlattenTree(treeHash)
	if err != nil {
		return // best-effort
	}
	for _, e := range entries {
		if !isSidecarPath(e.Path) {
			continue
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(e.Path))
		os.MkdirAll(filepath.Dir(absPath), 0o755)
		blob, err := r.Store.ReadBlob(e.BlobHash)
		if err != nil {
			continue
		}
		os.WriteFile(absPath, blob.Data, 0o644)
	}
}

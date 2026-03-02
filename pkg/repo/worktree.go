package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// writeWorktreeFileAtomic atomically writes a file in the given directory.
func writeWorktreeFileAtomic(dir, name, content string) error {
	target := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, name+".tmp.*")
	if err != nil {
		return fmt.Errorf("write %s: create temp: %w", name, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write %s: write: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write %s: sync: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write %s: close: %w", name, err)
	}
	return os.Rename(tmpPath, target)
}

// WorktreeInfo describes a single worktree (main or linked).
type WorktreeInfo struct {
	Name   string      // worktree name (directory name under .graft/worktrees/)
	Path   string      // absolute path to the worktree working directory
	Branch string      // branch checked out (e.g., "feature") or empty if detached
	Head   object.Hash // current HEAD hash
}

// IsLinkedWorktree returns true if this Repo was opened from a linked
// worktree, i.e. the .graft entry at the working directory root is a file
// rather than a directory.
func (r *Repo) IsLinkedWorktree() bool {
	return r.CommonDir != ""
}

// WorktreeAdd creates a linked worktree at path, checked out on the given
// branch. It returns a *Repo pointing at the new worktree.
func (r *Repo) WorktreeAdd(path, branch string) (*Repo, error) {
	// Cannot nest: linked worktrees cannot create further worktrees.
	if r.IsLinkedWorktree() {
		return nil, fmt.Errorf("worktree add: cannot add a worktree from a linked worktree")
	}

	// Resolve branch to a commit hash.
	branchHash, err := r.ResolveRef("refs/heads/" + branch)
	if err != nil {
		return nil, fmt.Errorf("worktree add: resolve branch %q: %w", branch, err)
	}

	// Make path absolute.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("worktree add: abs path: %w", err)
	}

	// Derive name from the last segment of path.
	name := filepath.Base(absPath)

	// Check that worktree metadata dir doesn't already exist.
	wtMetaDir := filepath.Join(r.GraftDir, "worktrees", name)
	if _, err := os.Stat(wtMetaDir); err == nil {
		return nil, fmt.Errorf("worktree add: worktree %q already exists", name)
	}

	// Create the worktree working directory.
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return nil, fmt.Errorf("worktree add: mkdir %q: %w", absPath, err)
	}

	// Create .graft/worktrees/<name>/ in the main repo.
	if err := os.MkdirAll(wtMetaDir, 0o755); err != nil {
		return nil, fmt.Errorf("worktree add: mkdir metadata: %w", err)
	}

	// Write HEAD for the worktree (symbolic ref to the branch).
	headContent := "ref: refs/heads/" + branch + "\n"
	if err := writeWorktreeFileAtomic(wtMetaDir, "HEAD", headContent); err != nil {
		return nil, fmt.Errorf("worktree add: write HEAD: %w", err)
	}

	// Write commondir: relative path from wtMetaDir back to main .graft/.
	commonRel, err := filepath.Rel(wtMetaDir, r.GraftDir)
	if err != nil {
		return nil, fmt.Errorf("worktree add: compute commondir: %w", err)
	}
	if err := writeWorktreeFileAtomic(wtMetaDir, "commondir", commonRel+"\n"); err != nil {
		return nil, fmt.Errorf("worktree add: write commondir: %w", err)
	}

	// Write path file: absolute path to the worktree working directory (for listing/removal).
	if err := writeWorktreeFileAtomic(wtMetaDir, "path", absPath+"\n"); err != nil {
		return nil, fmt.Errorf("worktree add: write path: %w", err)
	}

	// Write a .graft FILE (not directory) in the worktree path.
	graftFileContent := "gitdir: " + wtMetaDir + "\n"
	if err := os.WriteFile(filepath.Join(absPath, ".graft"), []byte(graftFileContent), 0o644); err != nil {
		return nil, fmt.Errorf("worktree add: write .graft file: %w", err)
	}

	// Build the worktree Repo for writing files and staging.
	wtRepo := &Repo{
		RootDir:   absPath,
		GraftDir:  wtMetaDir,
		CommonDir: r.GraftDir,
		Store:     object.NewStore(r.GraftDir),
	}

	// Read the commit and flatten its tree into the worktree directory.
	commit, err := wtRepo.Store.ReadCommit(branchHash)
	if err != nil {
		return nil, fmt.Errorf("worktree add: read commit %s: %w", branchHash, err)
	}

	files, err := wtRepo.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("worktree add: flatten tree: %w", err)
	}

	// Write files to the worktree working directory.
	for _, f := range files {
		absFilePath := filepath.Join(absPath, filepath.FromSlash(f.Path))
		dir := filepath.Dir(absFilePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("worktree add: mkdir %q: %w", dir, err)
		}
		blob, err := wtRepo.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return nil, fmt.Errorf("worktree add: read blob for %q: %w", f.Path, err)
		}
		if err := os.WriteFile(absFilePath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return nil, fmt.Errorf("worktree add: write %q: %w", f.Path, err)
		}
	}

	// Build staging index for the worktree.
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(files))}
	for _, f := range files {
		absFilePath := filepath.Join(absPath, filepath.FromSlash(f.Path))
		info, err := os.Stat(absFilePath)
		if err != nil {
			return nil, fmt.Errorf("worktree add: stat %q: %w", f.Path, err)
		}
		entry := &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
		}
		setStagingEntryStat(entry, info, normalizeFileMode(f.Mode))
		stg.Entries[f.Path] = entry
	}
	if err := wtRepo.WriteStaging(stg); err != nil {
		return nil, fmt.Errorf("worktree add: write staging: %w", err)
	}

	return wtRepo, nil
}

// WorktreeList returns information about all worktrees. The main worktree
// is always first, followed by any linked worktrees sorted by name.
func (r *Repo) WorktreeList() ([]WorktreeInfo, error) {
	// Determine the main repo's GraftDir and RootDir.
	mainGraftDir := r.GraftDir
	mainRootDir := r.RootDir
	if r.IsLinkedWorktree() {
		mainGraftDir = r.CommonDir
		mainRootDir = filepath.Dir(mainGraftDir)
	}

	var infos []WorktreeInfo

	// Main worktree entry.
	mainInfo := WorktreeInfo{
		Name: filepath.Base(mainRootDir),
		Path: mainRootDir,
	}
	// Read main HEAD.
	mainHeadData, err := os.ReadFile(filepath.Join(mainGraftDir, "HEAD"))
	if err == nil {
		headStr := strings.TrimRight(string(mainHeadData), "\n")
		if strings.HasPrefix(headStr, "ref: refs/heads/") {
			mainInfo.Branch = strings.TrimPrefix(headStr, "ref: refs/heads/")
			// Try to resolve the branch to a hash.
			refData, err := os.ReadFile(filepath.Join(mainGraftDir, "refs", "heads", mainInfo.Branch))
			if err == nil {
				mainInfo.Head = object.Hash(strings.TrimSpace(string(refData)))
			}
		} else if strings.HasPrefix(headStr, "ref: ") {
			// Some other ref format.
		} else {
			mainInfo.Head = object.Hash(headStr)
		}
	}
	infos = append(infos, mainInfo)

	// Read linked worktrees from .graft/worktrees/.
	worktreesDir := filepath.Join(mainGraftDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return infos, nil
		}
		return nil, fmt.Errorf("worktree list: read worktrees dir: %w", err)
	}

	// Sort entries by name.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		wtMetaDir := filepath.Join(worktreesDir, name)

		wi := WorktreeInfo{Name: name}

		// Read the worktree's working directory path from the "path" file
		// written during WorktreeAdd.

		pathData, err := os.ReadFile(filepath.Join(wtMetaDir, "path"))
		if err == nil {
			wi.Path = strings.TrimSpace(string(pathData))
		}

		// Read HEAD for this worktree.
		headData, err := os.ReadFile(filepath.Join(wtMetaDir, "HEAD"))
		if err == nil {
			headStr := strings.TrimRight(string(headData), "\n")
			if strings.HasPrefix(headStr, "ref: refs/heads/") {
				wi.Branch = strings.TrimPrefix(headStr, "ref: refs/heads/")
				// Resolve the branch to a hash from the shared refs.
				refData, err := os.ReadFile(filepath.Join(mainGraftDir, "refs", "heads", wi.Branch))
				if err == nil {
					wi.Head = object.Hash(strings.TrimSpace(string(refData)))
				}
			} else if strings.HasPrefix(headStr, "ref: ") {
				// other ref
			} else {
				wi.Head = object.Hash(headStr)
			}
		}

		infos = append(infos, wi)
	}

	return infos, nil
}

// WorktreeRemove removes a linked worktree by name. It removes both the
// worktree working directory and the metadata in .graft/worktrees/<name>/.
func (r *Repo) WorktreeRemove(name string) error {
	mainGraftDir := r.GraftDir
	if r.IsLinkedWorktree() {
		mainGraftDir = r.CommonDir
	}

	wtMetaDir := filepath.Join(mainGraftDir, "worktrees", name)
	if _, err := os.Stat(wtMetaDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree remove: worktree %q not found", name)
		}
		return fmt.Errorf("worktree remove: stat metadata: %w", err)
	}

	// Read the worktree path.
	pathData, err := os.ReadFile(filepath.Join(wtMetaDir, "path"))
	if err == nil {
		wtPath := strings.TrimSpace(string(pathData))
		if wtPath != "" {
			if err := os.RemoveAll(wtPath); err != nil {
				return fmt.Errorf("worktree remove: remove working directory %q: %w", wtPath, err)
			}
		}
	}

	// Remove the metadata directory.
	if err := os.RemoveAll(wtMetaDir); err != nil {
		return fmt.Errorf("worktree remove: remove metadata %q: %w", wtMetaDir, err)
	}

	return nil
}

// WorktreePrune removes stale worktree entries where the working directory
// no longer exists on disk.
func (r *Repo) WorktreePrune() error {
	mainGraftDir := r.GraftDir
	if r.IsLinkedWorktree() {
		mainGraftDir = r.CommonDir
	}

	worktreesDir := filepath.Join(mainGraftDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("worktree prune: read worktrees dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		wtMetaDir := filepath.Join(worktreesDir, name)

		pathData, err := os.ReadFile(filepath.Join(wtMetaDir, "path"))
		if err != nil {
			// No path file -- stale entry.
			if err := os.RemoveAll(wtMetaDir); err != nil {
				return fmt.Errorf("worktree prune: remove %q: %w", name, err)
			}
			continue
		}

		wtPath := strings.TrimSpace(string(pathData))
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			// Working directory gone -- stale.
			if err := os.RemoveAll(wtMetaDir); err != nil {
				return fmt.Errorf("worktree prune: remove %q: %w", name, err)
			}
		}
	}

	return nil
}

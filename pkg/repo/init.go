package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

var ErrRefCASMismatch = errors.New("ref compare-and-swap mismatch")
var ErrRefUpdatedButReflogAppendFailed = errors.New("ref updated but reflog append failed")

// RefUpdateReflogError indicates the ref file update succeeded, but appending
// the corresponding reflog entry failed.
type RefUpdateReflogError struct {
	Ref     string
	OldHash object.Hash
	NewHash object.Hash
	Err     error
}

func (e *RefUpdateReflogError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf(
		"update ref %q: %s (old=%s new=%s): %v",
		e.Ref,
		ErrRefUpdatedButReflogAppendFailed,
		e.OldHash,
		e.NewHash,
		e.Err,
	)
}

func (e *RefUpdateReflogError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *RefUpdateReflogError) Is(target error) bool {
	return target == ErrRefUpdatedButReflogAppendFailed
}

const (
	refLockRetryDelay = 5 * time.Millisecond
	refLockWaitLimit  = 2 * time.Second
)

// Init creates a new Graft repository at path. It creates the .graft/ directory
// structure: HEAD, objects/, and refs/heads/. Returns an error if a .graft/
// directory already exists.
func Init(path string) (*Repo, error) {
	graftDir := filepath.Join(path, ".graft")

	// Fail if .graft/ already exists.
	if _, err := os.Stat(graftDir); err == nil {
		return nil, fmt.Errorf("init: repository already exists at %s", graftDir)
	}

	// Create directory structure.
	dirs := []string{
		filepath.Join(graftDir, "objects"),
		filepath.Join(graftDir, "refs", "heads"),
		filepath.Join(graftDir, "logs", "refs", "heads"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("init: mkdir %s: %w", d, err)
		}
	}

	// Write default HEAD.
	headPath := filepath.Join(graftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		return nil, fmt.Errorf("init: write HEAD: %w", err)
	}

	return &Repo{
		RootDir: path,
		GraftDir: graftDir,
		Store:    object.NewStore(graftDir),
	}, nil
}

// Open searches upward from path for a .graft/ directory (or .graft file for
// linked worktrees, or .graft symlink for module working trees) and opens the
// repository. Returns an error if no .graft entry is found.
func Open(path string) (*Repo, error) {
	// Resolve to absolute path for consistent traversal.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open: abs path: %w", err)
	}

	cur := abs
	for {
		graftPath := filepath.Join(cur, ".graft")

		// Use Lstat so we can distinguish symlinks from regular
		// files/directories without following them.
		linfo, lerr := os.Lstat(graftPath)
		if lerr == nil {
			// 1. Real directory — normal repository.
			if linfo.IsDir() {
				return &Repo{
					RootDir:  cur,
					GraftDir: graftPath,
					Store:    object.NewStore(graftPath),
				}, nil
			}

			// 2. Symlink — check for module working tree.
			if linfo.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(graftPath)
				if err != nil {
					return nil, fmt.Errorf("open: readlink .graft: %w", err)
				}
				if strings.Contains(target, "/modules/") {
					return openModuleWorktree(cur, graftPath, target)
				}
				// Non-module symlink: follow it and treat as the
				// resolved type (directory or file).
				info, err := os.Stat(graftPath)
				if err != nil {
					return nil, fmt.Errorf("open: stat .graft symlink target: %w", err)
				}
				if info.IsDir() {
					return &Repo{
						RootDir:  cur,
						GraftDir: graftPath,
						Store:    object.NewStore(graftPath),
					}, nil
				}
				// Symlink to a file — treat as linked worktree.
				return openLinkedWorktree(cur, graftPath)
			}

			// 3. Regular file — linked worktree (gitdir: line).
			return openLinkedWorktree(cur, graftPath)
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding .graft/.
			return nil, fmt.Errorf("open: not a graft repository (or any parent up to /)")
		}
		cur = parent
	}
}

// openModuleWorktree opens a Repo configured for bidirectional development
// inside a module working tree. The .graft entry is a symlink whose target
// contains "/modules/", indicating it points to a per-module metadata
// directory under the parent repo's .graft/.
func openModuleWorktree(rootDir, symlinkPath, target string) (*Repo, error) {
	// The symlink is relative to the directory containing it.
	moduleDir := filepath.Dir(symlinkPath)
	metaDir := filepath.Join(moduleDir, target)
	metaDir = filepath.Clean(metaDir)

	// Derive the parent repo's .graft/ directory by stripping the
	// /modules/<name> suffix from the metadata dir path.
	// metaDir looks like: /path/to/repo/.graft/modules/mylib
	idx := strings.LastIndex(metaDir, string(filepath.Separator)+"modules"+string(filepath.Separator))
	if idx == -1 {
		return nil, fmt.Errorf("open: module metadata dir %q does not contain /modules/", metaDir)
	}
	parentGraftDir := metaDir[:idx]

	return &Repo{
		RootDir:   rootDir,
		GraftDir:  metaDir,
		CommonDir: parentGraftDir,
		Store:     object.NewStore(parentGraftDir),
	}, nil
}

// openLinkedWorktree opens a Repo from a linked worktree where .graft is a
// file containing "gitdir: <path-to-worktree-metadata>".
func openLinkedWorktree(rootDir, graftFile string) (*Repo, error) {
	data, err := os.ReadFile(graftFile)
	if err != nil {
		return nil, fmt.Errorf("open: read .graft file: %w", err)
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return nil, fmt.Errorf("open: invalid .graft file (expected 'gitdir: <path>')")
	}
	wtGraftDir := strings.TrimPrefix(content, "gitdir: ")

	// Read commondir from the worktree metadata directory.
	commondirData, err := os.ReadFile(filepath.Join(wtGraftDir, "commondir"))
	if err != nil {
		return nil, fmt.Errorf("open: read commondir: %w", err)
	}
	commonRel := strings.TrimSpace(string(commondirData))
	commonDir := filepath.Join(wtGraftDir, commonRel)
	// Clean the path to resolve any ".." segments.
	commonDir = filepath.Clean(commonDir)

	return &Repo{
		RootDir:   rootDir,
		GraftDir:  wtGraftDir,
		CommonDir: commonDir,
		Store:     object.NewStore(commonDir),
	}, nil
}

// writeHeadAtomic atomically writes the HEAD file using temp+fsync+rename.
func (r *Repo) writeHeadAtomic(content string) error {
	headPath := filepath.Join(r.GraftDir, "HEAD")
	tmpPath := headPath + ".lock"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write HEAD: create temp: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write HEAD: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write HEAD: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write HEAD: close: %w", err)
	}
	if err := os.Rename(tmpPath, headPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write HEAD: rename: %w", err)
	}
	return nil
}

// setHeadSymbolic atomically sets HEAD to a symbolic ref.
func (r *Repo) setHeadSymbolic(refName string) error {
	return r.writeHeadAtomic("ref: " + refName + "\n")
}

// setHeadDetached atomically sets HEAD to a detached commit hash.
func (r *Repo) setHeadDetached(hash object.Hash) error {
	return r.writeHeadAtomic(string(hash) + "\n")
}

// Head reads .graft/HEAD. If the content starts with "ref: ", it returns the
// ref path (e.g., "refs/heads/main"). Otherwise it returns the raw content
// as a detached hash string.
func (r *Repo) Head() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	content := strings.TrimRight(string(data), "\n")

	if strings.HasPrefix(content, "ref: ") {
		return strings.TrimPrefix(content, "ref: "), nil
	}
	return content, nil
}

// ResolveRef resolves a ref name to an object hash.
//
// Resolution order:
//  1. If name is "HEAD", read HEAD. If HEAD is symbolic, resolve the target ref.
//  2. If name starts with "refs/", read .graft/<name>.
//  3. Otherwise, try "refs/heads/<name>".
func (r *Repo) ResolveRef(name string) (object.Hash, error) {
	if name == "HEAD" {
		head, err := r.Head()
		if err != nil {
			return "", err
		}
		// If Head returned a ref path, resolve it recursively.
		if strings.HasPrefix(head, "refs/") {
			return r.ResolveRef(head)
		}
		// Detached HEAD: the value is a hash.
		return object.Hash(head), nil
	}

	// Determine the file to read. Refs are shared (use refsBaseDir), not
	// worktree-specific.
	var refPath string
	if strings.HasPrefix(name, "refs/") {
		refPath = filepath.Join(r.refsBaseDir(), name)
	} else {
		refPath = filepath.Join(r.refsBaseDir(), "refs", "heads", name)
	}

	data, err := os.ReadFile(refPath)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", name, err)
	}
	return object.Hash(strings.TrimRight(string(data), "\n")), nil
}

// UpdateRef writes a hash to the named ref file under .graft/. Parent
// directories are created as needed.
func (r *Repo) UpdateRef(name string, h object.Hash) error {
	return r.UpdateRefCAS(name, h)
}

// UpdateRefCAS writes a hash to the named ref file under .graft/ using
// lockfile + rename atomic semantics. If expectedOld is provided, the
// update only succeeds when the current ref hash matches it.
//
// Reflog append happens after the ref rename; if reflog append fails, the ref
// update remains committed and a RefUpdateReflogError is returned.
func (r *Repo) UpdateRefCAS(name string, h object.Hash, expectedOld ...object.Hash) error {
	if len(expectedOld) > 1 {
		return fmt.Errorf("update ref %q: expected at most one old hash", name)
	}
	hasExpectedOld := len(expectedOld) == 1
	wantOldHash := object.Hash("")
	if hasExpectedOld {
		wantOldHash = expectedOld[0]
	}

	// For HEAD, use GraftDir (worktree-specific). For all other refs, use
	// refsBaseDir (shared in linked worktrees).
	baseDir := r.refsBaseDir()
	if name == "HEAD" {
		baseDir = r.GraftDir
	}
	refPath := filepath.Join(baseDir, name)

	dir := filepath.Dir(refPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("update ref %q: mkdir: %w", name, err)
	}

	lockPath := refPath + ".lock"
	lockFile, err := acquireRefLock(lockPath)
	if err != nil {
		return fmt.Errorf("update ref %q: lock: %w", name, err)
	}
	cleanupLock := true
	defer func() {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		if cleanupLock {
			_ = os.Remove(lockPath)
		}
	}()

	oldHash, err := readRefHash(refPath)
	if err != nil {
		return fmt.Errorf("update ref %q: read old hash: %w", name, err)
	}
	if hasExpectedOld && oldHash != wantOldHash {
		return fmt.Errorf(
			"update ref %q: %w (expected %s, found %s)",
			name,
			ErrRefCASMismatch,
			wantOldHash,
			oldHash,
		)
	}

	if _, err := lockFile.WriteString(string(h) + "\n"); err != nil {
		return fmt.Errorf("update ref %q: write: %w", name, err)
	}
	if err := lockFile.Sync(); err != nil {
		return fmt.Errorf("update ref %q: sync: %w", name, err)
	}
	if err := lockFile.Close(); err != nil {
		lockFile = nil
		return fmt.Errorf("update ref %q: close: %w", name, err)
	}
	lockFile = nil

	if err := os.Rename(lockPath, refPath); err != nil {
		return fmt.Errorf("update ref %q: rename: %w", name, err)
	}
	cleanupLock = false
	r.InvalidateMergeBaseCache()

	if err := r.appendReflogAutoEntities(name, oldHash, h, "update"); err != nil {
		return &RefUpdateReflogError{
			Ref:     name,
			OldHash: oldHash,
			NewHash: h,
			Err:     err,
		}
	}

	return nil
}

func acquireRefLock(lockPath string) (*os.File, error) {
	deadline := time.Now().Add(refLockWaitLimit)
	for {
		f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, nil
		}
		if os.IsExist(err) {
			// Check for stale lock: if the lock file is older than 5 minutes,
			// it was likely left by a crashed process. Remove it and retry.
			if info, statErr := os.Stat(lockPath); statErr == nil {
				if time.Since(info.ModTime()) > 5*time.Minute {
					os.Remove(lockPath)
					continue
				}
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for lock %q (may be stale — remove manually if no graft process is running)", lockPath)
			}
			time.Sleep(refLockRetryDelay)
			continue
		}
		return nil, err
	}
}

func readRefHash(refPath string) (object.Hash, error) {
	data, err := os.ReadFile(refPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return object.Hash(strings.TrimSpace(string(data))), nil
}

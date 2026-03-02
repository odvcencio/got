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
	gotDir := filepath.Join(path, ".graft")

	// Fail if .graft/ already exists.
	if _, err := os.Stat(gotDir); err == nil {
		return nil, fmt.Errorf("init: repository already exists at %s", gotDir)
	}

	// Create directory structure.
	dirs := []string{
		filepath.Join(gotDir, "objects"),
		filepath.Join(gotDir, "refs", "heads"),
		filepath.Join(gotDir, "logs", "refs", "heads"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("init: mkdir %s: %w", d, err)
		}
	}

	// Write default HEAD.
	headPath := filepath.Join(gotDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		return nil, fmt.Errorf("init: write HEAD: %w", err)
	}

	return &Repo{
		RootDir: path,
		GotDir:  gotDir,
		Store:   object.NewStore(gotDir),
	}, nil
}

// Open searches upward from path for a .graft/ directory (or .graft file for
// linked worktrees) and opens the repository. Returns an error if no .graft
// entry is found.
func Open(path string) (*Repo, error) {
	// Resolve to absolute path for consistent traversal.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open: abs path: %w", err)
	}

	cur := abs
	for {
		gotDir := filepath.Join(cur, ".graft")
		info, err := os.Stat(gotDir)
		if err == nil {
			if info.IsDir() {
				// Normal repository.
				return &Repo{
					RootDir: cur,
					GotDir:  gotDir,
					Store:   object.NewStore(gotDir),
				}, nil
			}
			// .graft is a file -- this is a linked worktree.
			return openLinkedWorktree(cur, gotDir)
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding .graft/.
			return nil, fmt.Errorf("open: not a got repository (or any parent up to /)")
		}
		cur = parent
	}
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
	wtGotDir := strings.TrimPrefix(content, "gitdir: ")

	// Read commondir from the worktree metadata directory.
	commondirData, err := os.ReadFile(filepath.Join(wtGotDir, "commondir"))
	if err != nil {
		return nil, fmt.Errorf("open: read commondir: %w", err)
	}
	commonRel := strings.TrimSpace(string(commondirData))
	commonDir := filepath.Join(wtGotDir, commonRel)
	// Clean the path to resolve any ".." segments.
	commonDir = filepath.Clean(commonDir)

	return &Repo{
		RootDir:   rootDir,
		GotDir:    wtGotDir,
		CommonDir: commonDir,
		Store:     object.NewStore(commonDir),
	}, nil
}

// Head reads .graft/HEAD. If the content starts with "ref: ", it returns the
// ref path (e.g., "refs/heads/main"). Otherwise it returns the raw content
// as a detached hash string.
func (r *Repo) Head() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.GotDir, "HEAD"))
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

	// For HEAD, use GotDir (worktree-specific). For all other refs, use
	// refsBaseDir (shared in linked worktrees).
	baseDir := r.refsBaseDir()
	if name == "HEAD" {
		baseDir = r.GotDir
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

	if err := r.appendReflog(name, oldHash, h, "update"); err != nil {
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
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for lock %q", lockPath)
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

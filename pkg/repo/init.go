package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// Init creates a new Got repository at path. It creates the .got/ directory
// structure: HEAD, objects/, and refs/heads/. Returns an error if a .got/
// directory already exists.
func Init(path string) (*Repo, error) {
	gotDir := filepath.Join(path, ".got")

	// Fail if .got/ already exists.
	if _, err := os.Stat(gotDir); err == nil {
		return nil, fmt.Errorf("init: repository already exists at %s", gotDir)
	}

	// Create directory structure.
	dirs := []string{
		filepath.Join(gotDir, "objects"),
		filepath.Join(gotDir, "refs", "heads"),
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

// Open searches upward from path for a .got/ directory and opens the
// repository. Returns an error if no .got/ directory is found.
func Open(path string) (*Repo, error) {
	// Resolve to absolute path for consistent traversal.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open: abs path: %w", err)
	}

	cur := abs
	for {
		gotDir := filepath.Join(cur, ".got")
		info, err := os.Stat(gotDir)
		if err == nil && info.IsDir() {
			return &Repo{
				RootDir: cur,
				GotDir:  gotDir,
				Store:   object.NewStore(gotDir),
			}, nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding .got/.
			return nil, fmt.Errorf("open: not a got repository (or any parent up to /)")
		}
		cur = parent
	}
}

// Head reads .got/HEAD. If the content starts with "ref: ", it returns the
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
//  2. If name starts with "refs/", read .got/<name>.
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

	// Determine the file to read.
	var refPath string
	if strings.HasPrefix(name, "refs/") {
		refPath = filepath.Join(r.GotDir, name)
	} else {
		refPath = filepath.Join(r.GotDir, "refs", "heads", name)
	}

	data, err := os.ReadFile(refPath)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", name, err)
	}
	return object.Hash(strings.TrimRight(string(data), "\n")), nil
}

// UpdateRef writes a hash to the named ref file under .got/. Parent
// directories are created as needed. The write is atomic: data is written
// to a temporary file and then renamed into place.
func (r *Repo) UpdateRef(name string, h object.Hash) error {
	refPath := filepath.Join(r.GotDir, name)

	dir := filepath.Dir(refPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("update ref %q: mkdir: %w", name, err)
	}

	// Atomic write: temp file + rename.
	tmp, err := os.CreateTemp(dir, ".ref-tmp-*")
	if err != nil {
		return fmt.Errorf("update ref %q: tmpfile: %w", name, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(string(h) + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("update ref %q: write: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("update ref %q: close: %w", name, err)
	}

	if err := os.Rename(tmpName, refPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("update ref %q: rename: %w", name, err)
	}

	return nil
}

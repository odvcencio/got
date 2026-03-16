package gitbridge

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// Bridge manages the bidirectional relationship between .git/ and .graft/.
type Bridge struct {
	rootDir  string
	gitDir   string
	graftDir string
	hashMap  *HashMap
	store    *object.Store
}

// DetectGitRepo reports whether dir contains a .git/HEAD file, indicating a
// git repository root.
func DetectGitRepo(dir string) bool {
	head := filepath.Join(dir, ".git", "HEAD")
	_, err := os.Stat(head)
	return err == nil
}

// InitBridge creates the .graft/ directory alongside .git/, initializes the
// object store and hash map, then imports the current HEAD snapshot.
func InitBridge(dir string) (*Bridge, error) {
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return nil, fmt.Errorf("no .git directory found in %s", dir)
	}

	graftDir := filepath.Join(dir, ".graft")

	// Create required .graft/ subdirectories.
	for _, sub := range []string{
		"objects",
		filepath.Join("refs", "heads"),
		filepath.Join("refs", "tags"),
		"info",
	} {
		if err := os.MkdirAll(filepath.Join(graftDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create .graft/%s: %w", sub, err)
		}
	}

	// Write .graft/HEAD, mirroring the current git branch when possible.
	headContent := "ref: refs/heads/main\n"
	if gitHeadData, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
		line := strings.TrimRight(string(gitHeadData), "\n")
		if strings.HasPrefix(line, "ref: refs/heads/") {
			headContent = line + "\n"
		}
	}
	if err := os.WriteFile(filepath.Join(graftDir, "HEAD"), []byte(headContent), 0o644); err != nil {
		return nil, fmt.Errorf("write .graft/HEAD: %w", err)
	}

	store := object.NewStore(graftDir)

	hashMapPath := filepath.Join(graftDir, "hashmap")
	hm, err := OpenHashMap(hashMapPath)
	if err != nil {
		return nil, fmt.Errorf("open hash map: %w", err)
	}

	b := &Bridge{
		rootDir:  dir,
		gitDir:   gitDir,
		graftDir: graftDir,
		hashMap:  hm,
		store:    store,
	}

	if err := b.addToGitExclude(); err != nil {
		// Non-fatal: best effort.
		_ = err
	}

	if err := b.importHEAD(); err != nil {
		_ = hm.Close()
		return nil, fmt.Errorf("import HEAD: %w", err)
	}

	return b, nil
}

// addToGitExclude adds ".graft/" to .git/info/exclude so git ignores the
// .graft directory without modifying .gitignore.
func (b *Bridge) addToGitExclude() error {
	infoDir := filepath.Join(b.gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create .git/info: %w", err)
	}

	excludePath := filepath.Join(infoDir, "exclude")

	// Read existing content to check if .graft/ is already excluded.
	existing, _ := os.ReadFile(excludePath)
	scanner := bufio.NewScanner(bytes.NewReader(existing))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == ".graft/" {
			return nil // already present
		}
	}

	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open exclude file: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintln(f, ".graft/")
	return err
}

// importHEAD runs `git ls-files` to enumerate tracked files, then stores each
// as a graft blob object, extracts entities where possible, and records the
// git↔graft hash mapping.
func (b *Bridge) importHEAD() error {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = b.rootDir
	out, err := cmd.Output()
	if err != nil {
		// Empty repo or no commits — not an error.
		return nil
	}

	if len(out) == 0 {
		return nil
	}

	// git ls-files -z separates entries with NUL bytes.
	files := strings.Split(string(out), "\x00")

	for _, relPath := range files {
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}

		absPath := filepath.Join(b.rootDir, relPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			// File may have been deleted since ls-files ran; skip it.
			continue
		}

		// Store as a graft blob.
		graftHash, err := b.store.Write(object.TypeBlob, content)
		if err != nil {
			return fmt.Errorf("store blob %s: %w", relPath, err)
		}

		// Compute the git blob hash for the same content.
		gitHash := GitObjectHash("blob", content)

		// Record the bidirectional mapping.
		if err := b.hashMap.Put(graftHash, gitHash); err != nil {
			return fmt.Errorf("record hash mapping for %s: %w", relPath, err)
		}

		// Entity extraction is deferred to `graft add` where results are
		// actually stored.  Running it here during init is wasteful — the
		// results were previously discarded.
	}

	return nil
}

// OpenBridge opens an existing bridge (does not create one).
func OpenBridge(dir string) (*Bridge, error) {
	graftDir := filepath.Join(dir, ".graft")
	if _, err := os.Stat(graftDir); err != nil {
		return nil, fmt.Errorf("no .graft directory found")
	}
	if !DetectGitRepo(dir) {
		return nil, fmt.Errorf("no .git directory found")
	}

	store := object.NewStore(graftDir)

	hm, err := OpenHashMap(filepath.Join(graftDir, "hashmap"))
	if err != nil {
		return nil, err
	}

	return &Bridge{
		rootDir:  dir,
		gitDir:   filepath.Join(dir, ".git"),
		graftDir: graftDir,
		hashMap:  hm,
		store:    store,
	}, nil
}

// IsBridgeRepo returns true if dir has both .git/ and .graft/.
func IsBridgeRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".graft"))
	return err == nil && DetectGitRepo(dir)
}

// Close releases resources held by the Bridge.
func (b *Bridge) Close() error {
	return b.hashMap.Close()
}

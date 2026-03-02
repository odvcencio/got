package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ancestorOp represents a single step in a revision suffix chain.
// tilde=true means ~N (follow first parent N times); tilde=false means ^N
// (take the Nth parent, 1-indexed).
type ancestorOp struct {
	tilde bool
	n     int
}

// parseRevisionSuffix splits a treeish string like "HEAD~3^2" into its base
// ref ("HEAD") and an ordered slice of ancestor operations ([~3, ^2]).
//
// Supported syntax (matching Git):
//
//	~N  — follow first parent N times
//	~   — shorthand for ~1
//	^N  — take Nth parent (1-indexed)
//	^   — shorthand for ^1
//	~~  — equivalent to ~2
//	^^  — equivalent to ~2 (each ^ is a first-parent step)
//	@   — alias for HEAD (resolved before parsing suffixes)
//
// Multiple operators can be chained: HEAD~3^2 parses as [~3, ^2].
func parseRevisionSuffix(spec string) (base string, ops []ancestorOp) {
	// Replace leading @ with HEAD.
	if spec == "@" || strings.HasPrefix(spec, "@~") || strings.HasPrefix(spec, "@^") {
		spec = "HEAD" + spec[1:]
	}

	// Find the first ~ or ^ to locate the boundary between base and suffix.
	suffixStart := -1
	for i, ch := range spec {
		if ch == '~' || ch == '^' {
			suffixStart = i
			break
		}
	}
	if suffixStart < 0 {
		return spec, nil
	}

	base = spec[:suffixStart]
	suffix := spec[suffixStart:]

	i := 0
	for i < len(suffix) {
		ch := suffix[i]
		if ch != '~' && ch != '^' {
			// Shouldn't happen if the spec is well-formed, but be defensive.
			break
		}
		isTilde := ch == '~'
		i++

		// Collect any digits following the operator.
		numStart := i
		for i < len(suffix) && suffix[i] >= '0' && suffix[i] <= '9' {
			i++
		}

		if numStart < i {
			// Explicit number given.
			n, _ := strconv.Atoi(suffix[numStart:i])
			ops = append(ops, ancestorOp{tilde: isTilde, n: n})
		} else {
			// Bare operator: ~ means ~1, ^ means ^1.
			ops = append(ops, ancestorOp{tilde: isTilde, n: 1})
		}
	}

	return base, ops
}

// walkAncestors applies a sequence of ancestor operations to a commit hash,
// walking the commit graph via the object store.
func (r *Repo) walkAncestors(h object.Hash, ops []ancestorOp) (object.Hash, error) {
	current := h
	for _, op := range ops {
		if op.tilde {
			// ~N: follow first parent N times.
			for step := 0; step < op.n; step++ {
				c, err := r.Store.ReadCommit(current)
				if err != nil {
					return "", fmt.Errorf("walk ancestors: read commit %s: %w", current, err)
				}
				if len(c.Parents) == 0 {
					return "", fmt.Errorf("walk ancestors: commit %s has no parents (needed %d more first-parent steps)", current, op.n-step)
				}
				current = c.Parents[0]
			}
		} else {
			// ^N: take Nth parent (1-indexed). ^0 means the commit itself.
			if op.n == 0 {
				continue
			}
			c, err := r.Store.ReadCommit(current)
			if err != nil {
				return "", fmt.Errorf("walk ancestors: read commit %s: %w", current, err)
			}
			if op.n > len(c.Parents) {
				return "", fmt.Errorf("walk ancestors: commit %s has %d parent(s), requested parent %d", current, len(c.Parents), op.n)
			}
			current = c.Parents[op.n-1]
		}
	}
	return current, nil
}

// refsBaseDir returns the directory used for shared refs. For a linked
// worktree this is CommonDir (the main .graft/); for a normal repo it is
// GraftDir. HEAD and index always live in GraftDir.
func (r *Repo) refsBaseDir() string {
	if r.CommonDir != "" {
		return r.CommonDir
	}
	return r.GraftDir
}

// ResolveTreeish resolves a treeish string to a commit hash. It supports
// ancestor notation (e.g., HEAD~3, main^2, HEAD~2^2, @~1) matching Git
// syntax. It tries, in order: refs/tags/<base>, refs/heads/<base>, HEAD
// (if base is "HEAD"), and finally treats the value as a raw hash — then
// applies any ancestor suffix operations to walk the commit graph.
func (r *Repo) ResolveTreeish(treeish string) (object.Hash, error) {
	// Parse ancestor suffix (e.g., "HEAD~3^2" → base="HEAD", ops=[~3,^2]).
	base, ops := parseRevisionSuffix(treeish)

	// Resolve the base ref.
	h, err := r.resolveBaseTreeish(base)
	if err != nil {
		return "", fmt.Errorf("cannot resolve treeish %q", treeish)
	}

	// If no suffix operations, return the resolved hash directly.
	if len(ops) == 0 {
		return h, nil
	}

	// Walk the commit graph according to the ancestor operations.
	result, err := r.walkAncestors(h, ops)
	if err != nil {
		return "", fmt.Errorf("resolve treeish %q: %w", treeish, err)
	}
	return result, nil
}

// resolveBaseTreeish resolves a base ref string (without ancestor suffix)
// to a commit hash using the standard resolution order: tag, branch, raw
// ref, raw hash.
func (r *Repo) resolveBaseTreeish(base string) (object.Hash, error) {
	// Try tag ref first.
	if h, err := r.ResolveRef("refs/tags/" + base); err == nil {
		return h, nil
	}
	// Try branch ref.
	if h, err := r.ResolveRef("refs/heads/" + base); err == nil {
		return h, nil
	}
	// Try as-is (covers HEAD and full ref paths).
	if h, err := r.ResolveRef(base); err == nil {
		return h, nil
	}
	// Treat as raw hash: verify the commit exists.
	h := object.Hash(base)
	if _, err := r.Store.ReadCommit(h); err == nil {
		return h, nil
	}
	return "", fmt.Errorf("cannot resolve base ref %q", base)
}

// ListRefs lists references under .graft/refs.
// Names are returned relative to refs root, e.g. "heads/main", "tags/v1".
func (r *Repo) ListRefs(prefix string) (map[string]object.Hash, error) {
	root := filepath.Join(r.refsBaseDir(), "refs")
	dir := root
	if strings.TrimSpace(prefix) != "" {
		dir = filepath.Join(root, filepath.FromSlash(prefix))
	}

	refs := make(map[string]object.Hash)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		refs[name] = object.Hash(strings.TrimSpace(string(data)))
		return nil
	})
	if os.IsNotExist(err) {
		return refs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list refs: %w", err)
	}
	return refs, nil
}

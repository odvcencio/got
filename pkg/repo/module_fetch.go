package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// ModuleFetchResult describes the outcome of a single module fetch-and-update.
type ModuleFetchResult struct {
	Name        string
	OldCommit   object.Hash
	NewCommit   object.Hash
	ObjectCount int
	Changed     bool
}

// ModuleFetchAndUpdate fetches the latest commit for a module from its remote,
// writes any new objects into the local store, and updates the lock file.
//
// The resolvedURL should be the canonicalized remote URL (e.g. expanded from
// shorthand). If resolvedURL is empty, the module's configured URL is used.
//
// When depth > 0 the fetch is shallow, limiting commit history to the given
// number of ancestors. depth == 0 performs a full fetch (the default).
func (r *Repo) ModuleFetchAndUpdate(ctx context.Context, name, resolvedURL string, depth int) (*ModuleFetchResult, error) {
	m, err := r.GetModule(name)
	if err != nil {
		return nil, err
	}

	if resolvedURL == "" {
		resolvedURL = m.URL
	}

	client, err := remote.NewClient(resolvedURL)
	if err != nil {
		return nil, fmt.Errorf("module %s: create remote client: %w", name, err)
	}

	refs, err := client.ListRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("module %s: list refs: %w", name, err)
	}

	targetHash, err := resolveModuleTarget(m, refs)
	if err != nil {
		return nil, fmt.Errorf("module %s: %w", name, err)
	}

	result := &ModuleFetchResult{
		Name:      name,
		OldCommit: m.Commit,
		NewCommit: targetHash,
	}

	// Already at the target commit -- nothing to do.
	if m.Commit == targetHash {
		return result, nil
	}

	// Fetch objects reachable from target into the local store.
	var haves []object.Hash
	if m.Commit != "" {
		haves = append(haves, m.Commit)
	}

	cfg := remote.FetchConfig{Depth: depth}
	fetchResult, err := remote.FetchIntoStoreShallow(ctx, client, r.Store, []object.Hash{targetHash}, haves, cfg)
	if err != nil {
		return nil, fmt.Errorf("module %s: fetch objects: %w", name, err)
	}
	result.ObjectCount = fetchResult.Written
	result.Changed = true

	// Update the lock file to record the new commit.
	if err := r.UpdateModuleLock(name, targetHash, resolvedURL); err != nil {
		return nil, fmt.Errorf("module %s: update lock: %w", name, err)
	}

	return result, nil
}

// resolveModuleTarget determines the target commit hash for a module based on
// its tracking configuration and the remote's advertised refs.
func resolveModuleTarget(m *Module, refs map[string]object.Hash) (object.Hash, error) {
	if m.Track != "" {
		refName := "refs/heads/" + m.Track
		h, ok := refs[refName]
		if !ok {
			return "", fmt.Errorf("tracking branch %q not found in remote refs", m.Track)
		}
		return h, nil
	}

	if m.Pin != "" {
		// Try tag ref first.
		tagRef := "refs/tags/" + m.Pin
		if h, ok := refs[tagRef]; ok {
			return h, nil
		}
		// Fall back to treating pin as a literal commit hash.
		pin := strings.TrimSpace(m.Pin)
		if len(pin) >= 8 {
			return object.Hash(pin), nil
		}
		return "", fmt.Errorf("pin %q not found as tag and too short to be a commit hash", m.Pin)
	}

	return "", fmt.Errorf("module has neither track nor pin configured")
}

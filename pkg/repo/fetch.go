package repo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// RefUpdate describes how a single reference changed during a fetch.
type RefUpdate struct {
	Name    string      // tracking ref name, e.g. "refs/remotes/origin/heads/main"
	OldHash object.Hash // previous value ("" if newly created)
	NewHash object.Hash // current value after fetch
}

// FetchResult summarizes the outcome of a Fetch operation.
type FetchResult struct {
	RemoteName  string
	RemoteURL   string
	UpdatedRefs []RefUpdate
	ObjectCount int // number of new objects written to the store
}

// Fetch downloads objects and refs from the named remote without modifying
// the working tree or current branch. Remote refs are stored under
// refs/remotes/<remoteName>/.
//
// For local-path remotes the source repository is opened directly and objects
// are copied by walking the object graph. For HTTP remotes the existing
// remote.Client + FetchIntoStore protocol is used.
func (r *Repo) Fetch(remoteName string) (*FetchResult, error) {
	return r.FetchContext(context.Background(), remoteName)
}

// FetchContext is like Fetch but accepts an explicit context.
func (r *Repo) FetchContext(ctx context.Context, remoteName string) (*FetchResult, error) {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		remoteName = "origin"
	}

	remoteURL, err := r.RemoteURL(remoteName)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	result := &FetchResult{
		RemoteName: remoteName,
		RemoteURL:  remoteURL,
	}

	// Determine whether the remote is a local path or an HTTP endpoint.
	if isLocalPath(remoteURL) {
		if err := r.fetchFromLocal(ctx, remoteName, remoteURL, result); err != nil {
			return nil, err
		}
		return result, nil
	}

	if err := r.fetchFromRemote(ctx, remoteName, remoteURL, result); err != nil {
		return nil, err
	}
	return result, nil
}

// isLocalPath returns true when the URL looks like a filesystem path rather
// than an HTTP(S) endpoint.
func isLocalPath(url string) bool {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return false
	}
	// Absolute or relative filesystem path.
	return true
}

// fetchFromLocal fetches from a local graft repository by opening it,
// listing its refs, and copying the full object graph.
func (r *Repo) fetchFromLocal(_ context.Context, remoteName, path string, result *FetchResult) error {
	srcRepo, err := Open(path)
	if err != nil {
		return fmt.Errorf("fetch: open local remote %q: %w", path, err)
	}

	// List the source repo's refs.
	srcRefs, err := srcRepo.ListRefs("")
	if err != nil {
		return fmt.Errorf("fetch: list remote refs: %w", err)
	}

	if len(srcRefs) == 0 {
		return nil
	}

	// Collect all ref tip hashes we need to fetch.
	wants := make([]object.Hash, 0, len(srcRefs))
	for _, h := range srcRefs {
		if strings.TrimSpace(string(h)) != "" {
			wants = append(wants, h)
		}
	}

	// Copy objects by walking the graph from each want root.
	written := 0
	for _, wantHash := range wants {
		n, err := copyObjectGraph(srcRepo.Store, r.Store, wantHash)
		if err != nil {
			return fmt.Errorf("fetch: copy objects: %w", err)
		}
		written += n
	}
	result.ObjectCount = written

	// Update tracking refs.
	for refName, h := range srcRefs {
		trackingRef := trackingRefName(remoteName, refName)
		oldHash, _ := r.ResolveRef(trackingRef)
		if oldHash == h {
			continue // already up to date
		}
		if err := r.UpdateRef(trackingRef, h); err != nil {
			return fmt.Errorf("fetch: update tracking ref %q: %w", trackingRef, err)
		}
		result.UpdatedRefs = append(result.UpdatedRefs, RefUpdate{
			Name:    trackingRef,
			OldHash: oldHash,
			NewHash: h,
		})
	}

	return nil
}

// fetchFromRemote fetches from an HTTP remote using the graft protocol client.
func (r *Repo) fetchFromRemote(ctx context.Context, remoteName, remoteURL string, result *FetchResult) error {
	client, err := remote.NewClient(remoteURL)
	if err != nil {
		return fmt.Errorf("fetch: create client: %w", err)
	}

	remoteRefs, err := client.ListRefs(ctx)
	if err != nil {
		return fmt.Errorf("fetch: list remote refs: %w", err)
	}

	if len(remoteRefs) == 0 {
		return nil
	}

	// Collect wants from all remote refs.
	wants := make([]object.Hash, 0, len(remoteRefs))
	for _, h := range remoteRefs {
		if strings.TrimSpace(string(h)) != "" {
			wants = append(wants, h)
		}
	}

	// Collect local haves from all existing refs.
	haves, err := r.localRefTips()
	if err != nil {
		return fmt.Errorf("fetch: collect local refs: %w", err)
	}

	// Fetch objects into store.
	if len(wants) > 0 {
		written, err := remote.FetchIntoStore(ctx, client, r.Store, wants, haves)
		if err != nil {
			return fmt.Errorf("fetch: download objects: %w", err)
		}
		result.ObjectCount = written
	}

	// Update tracking refs.
	for refName, h := range remoteRefs {
		trackingRef := trackingRefName(remoteName, refName)
		oldHash, _ := r.ResolveRef(trackingRef)
		if oldHash == h {
			continue
		}
		if err := r.UpdateRef(trackingRef, h); err != nil {
			return fmt.Errorf("fetch: update tracking ref %q: %w", trackingRef, err)
		}
		result.UpdatedRefs = append(result.UpdatedRefs, RefUpdate{
			Name:    trackingRef,
			OldHash: oldHash,
			NewHash: h,
		})
	}

	return nil
}

// localRefTips returns hash tips from all local refs for have negotiation.
func (r *Repo) localRefTips() ([]object.Hash, error) {
	refs, err := r.ListRefs("")
	if err != nil {
		return nil, err
	}
	tips := make([]object.Hash, 0, len(refs))
	for _, h := range refs {
		if strings.TrimSpace(string(h)) != "" {
			tips = append(tips, h)
		}
	}
	return tips, nil
}

// trackingRefName converts a remote ref name (e.g. "heads/main") into a
// local tracking ref path (e.g. "refs/remotes/origin/heads/main").
func trackingRefName(remoteName, refName string) string {
	return fmt.Sprintf("refs/remotes/%s/%s", remoteName, strings.TrimPrefix(refName, "/"))
}

// copyObjectGraph walks the object graph starting from root in the source
// store and copies all reachable objects into the destination store. It
// returns the number of new objects written.
func copyObjectGraph(src, dst *object.Store, root object.Hash) (int, error) {
	if strings.TrimSpace(string(root)) == "" {
		return 0, nil
	}

	written := 0
	seen := make(map[object.Hash]struct{})
	stack := []object.Hash{root}

	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if strings.TrimSpace(string(h)) == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}

		// Skip if destination already has the object.
		if dst.Has(h) {
			// Still need to check children aren't missing — but if the
			// destination already has this object we assume it has the full
			// subgraph (same assumption as git). Skipping avoids re-reading.
			continue
		}

		objType, data, err := src.Read(h)
		if err != nil {
			if os.IsNotExist(err) {
				continue // dangling reference; skip
			}
			return written, fmt.Errorf("read object %s: %w", h, err)
		}

		if _, err := dst.Write(objType, data); err != nil {
			return written, fmt.Errorf("write object %s: %w", h, err)
		}
		written++

		// Walk children.
		children, err := objectChildren(objType, data)
		if err != nil {
			return written, fmt.Errorf("parse object %s (%s): %w", h, objType, err)
		}
		stack = append(stack, children...)
	}

	return written, nil
}

// objectChildren returns the hashes directly referenced by an object.
func objectChildren(objType object.ObjectType, data []byte) ([]object.Hash, error) {
	switch objType {
	case object.TypeBlob, object.TypeEntity:
		return nil, nil
	case object.TypeTag:
		tag, err := object.UnmarshalTag(data)
		if err != nil {
			return nil, err
		}
		return []object.Hash{tag.TargetHash}, nil
	case object.TypeCommit:
		commit, err := object.UnmarshalCommit(data)
		if err != nil {
			return nil, err
		}
		refs := make([]object.Hash, 0, 1+len(commit.Parents))
		refs = append(refs, commit.TreeHash)
		refs = append(refs, commit.Parents...)
		return refs, nil
	case object.TypeTree:
		tree, err := object.UnmarshalTree(data)
		if err != nil {
			return nil, err
		}
		refs := make([]object.Hash, 0, len(tree.Entries)*2)
		for _, e := range tree.Entries {
			if e.IsDir {
				refs = append(refs, e.SubtreeHash)
				continue
			}
			refs = append(refs, e.BlobHash)
			if e.EntityListHash != "" {
				refs = append(refs, e.EntityListHash)
			}
		}
		return refs, nil
	case object.TypeEntityList:
		el, err := object.UnmarshalEntityList(data)
		if err != nil {
			return nil, err
		}
		refs := make([]object.Hash, 0, len(el.EntityRefs))
		refs = append(refs, el.EntityRefs...)
		return refs, nil
	default:
		return nil, fmt.Errorf("unsupported object type %q", objType)
	}
}

package remote

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

const (
	// MaxBatchObjects mirrors gothub's current server-side cap.
	MaxBatchObjects = 50000
	// MaxBatchHaveHashes keeps batch request payloads under server body limits.
	MaxBatchHaveHashes = 20000
	// MaxBatchNegotiationRounds prevents unbounded negotiation loops.
	MaxBatchNegotiationRounds = 1024
)

// FetchIntoStore fetches all objects reachable from wants into the local store.
//
// It starts with batch negotiation, then guarantees closure by walking the
// object graph locally and fetching any still-missing object via GetObject.
func FetchIntoStore(ctx context.Context, c *Client, store *object.Store, wants, haves []object.Hash) (int, error) {
	roots := uniqueHashes(wants)
	if len(roots) == 0 {
		return 0, fmt.Errorf("at least one want hash is required")
	}

	knownHaves, knownHaveSet := initKnownHaves(haves)
	written := 0
	negotiationCompleted := false
	for round := 0; round < MaxBatchNegotiationRounds; round++ {
		batchObjects, truncated, err := c.BatchObjects(ctx, roots, selectBatchHaves(knownHaves, MaxBatchHaveHashes), MaxBatchObjects)
		if err != nil {
			return written, err
		}

		newInRound := 0
		for _, obj := range batchObjects {
			n, err := writeVerifiedObject(store, obj)
			if err != nil {
				return written, err
			}
			written += n
			if n > 0 {
				newInRound++
			}
			knownHaves, knownHaveSet = appendKnownHave(knownHaves, knownHaveSet, obj.Hash)
		}

		if !truncated {
			negotiationCompleted = true
			break
		}
		// If the server keeps truncating without new objects, finish via point
		// fetches to avoid spinning on duplicate batches.
		if newInRound == 0 {
			negotiationCompleted = true
			break
		}
	}
	if !negotiationCompleted {
		return written, fmt.Errorf("batch negotiation exceeded %d rounds", MaxBatchNegotiationRounds)
	}

	// Always run closure for robustness against partial state and truncated batches.
	n, err := ensureGraphClosure(ctx, c, store, roots)
	if err != nil {
		return written, err
	}
	written += n
	return written, nil
}

func initKnownHaves(haves []object.Hash) ([]object.Hash, map[object.Hash]struct{}) {
	haveSet := make(map[object.Hash]struct{}, len(haves))
	haveList := make([]object.Hash, 0, len(haves))
	for _, h := range uniqueHashes(haves) {
		haveList = append(haveList, h)
		haveSet[h] = struct{}{}
	}
	return haveList, haveSet
}

func appendKnownHave(haveList []object.Hash, haveSet map[object.Hash]struct{}, h object.Hash) ([]object.Hash, map[object.Hash]struct{}) {
	h = object.Hash(strings.TrimSpace(string(h)))
	if h == "" {
		return haveList, haveSet
	}
	if _, ok := haveSet[h]; ok {
		return haveList, haveSet
	}
	haveSet[h] = struct{}{}
	haveList = append(haveList, h)
	return haveList, haveSet
}

func selectBatchHaves(haves []object.Hash, max int) []object.Hash {
	if max <= 0 || len(haves) <= max {
		out := make([]object.Hash, len(haves))
		copy(out, haves)
		return out
	}
	out := make([]object.Hash, max)
	copy(out, haves[len(haves)-max:])
	return out
}

// CollectObjectsForPush returns objects reachable from roots excluding objects
// in stopRoots (and anything reachable from stopRoots).
func CollectObjectsForPush(store *object.Store, roots, stopRoots []object.Hash) ([]ObjectRecord, error) {
	roots = uniqueHashes(roots)
	if len(roots) == 0 {
		return nil, fmt.Errorf("at least one root hash is required")
	}

	stopSet, err := ReachableSet(store, stopRoots)
	if err != nil {
		return nil, err
	}

	seen := make(map[object.Hash]struct{})
	stack := make([]object.Hash, 0, len(roots))
	stack = append(stack, roots...)

	objects := make([]ObjectRecord, 0, 1024)
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		if _, stopped := stopSet[h]; stopped {
			continue
		}
		seen[h] = struct{}{}

		objType, data, err := store.Read(h)
		if err != nil {
			return nil, fmt.Errorf("read object %s: %w", h, err)
		}
		objects = append(objects, ObjectRecord{Hash: h, Type: objType, Data: data})

		refs, err := referencedHashes(objType, data)
		if err != nil {
			return nil, fmt.Errorf("parse object %s (%s): %w", h, objType, err)
		}
		stack = append(stack, refs...)
	}

	return objects, nil
}

// ReachableSet returns all local object hashes reachable from roots.
// Missing roots are ignored.
func ReachableSet(store *object.Store, roots []object.Hash) (map[object.Hash]struct{}, error) {
	roots = uniqueHashes(roots)
	out := make(map[object.Hash]struct{}, len(roots))
	if len(roots) == 0 {
		return out, nil
	}

	stack := make([]object.Hash, 0, len(roots))
	stack = append(stack, roots...)

	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h == "" {
			continue
		}
		if _, ok := out[h]; ok {
			continue
		}
		if !store.Has(h) {
			continue
		}
		out[h] = struct{}{}

		objType, data, err := store.Read(h)
		if err != nil {
			return nil, fmt.Errorf("read object %s: %w", h, err)
		}
		refs, err := referencedHashes(objType, data)
		if err != nil {
			return nil, fmt.Errorf("parse object %s (%s): %w", h, objType, err)
		}
		stack = append(stack, refs...)
	}

	return out, nil
}

func ensureGraphClosure(ctx context.Context, c *Client, store *object.Store, roots []object.Hash) (int, error) {
	written := 0
	seen := make(map[object.Hash]struct{}, len(roots))
	stack := make([]object.Hash, 0, len(roots))
	stack = append(stack, roots...)

	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}

		if !store.Has(h) {
			obj, err := c.GetObject(ctx, h)
			if err != nil {
				return written, err
			}
			n, err := writeVerifiedObject(store, obj)
			if err != nil {
				return written, err
			}
			written += n
		}

		objType, data, err := store.Read(h)
		if err != nil {
			return written, fmt.Errorf("read object %s: %w", h, err)
		}
		refs, err := referencedHashes(objType, data)
		if err != nil {
			return written, fmt.Errorf("parse object %s (%s): %w", h, objType, err)
		}
		stack = append(stack, refs...)
	}

	return written, nil
}

func writeVerifiedObject(store *object.Store, obj ObjectRecord) (int, error) {
	if strings.TrimSpace(string(obj.Hash)) == "" {
		return 0, fmt.Errorf("object hash is required")
	}
	if _, err := parseObjectType(string(obj.Type)); err != nil {
		return 0, err
	}
	computed := object.HashObject(obj.Type, obj.Data)
	if computed != obj.Hash {
		return 0, fmt.Errorf("object hash mismatch: expected %s, got %s", obj.Hash, computed)
	}
	alreadyPresent := store.Has(obj.Hash)
	writtenHash, err := store.Write(obj.Type, obj.Data)
	if err != nil {
		return 0, err
	}
	if writtenHash != obj.Hash {
		return 0, fmt.Errorf("object write mismatch: expected %s, wrote %s", obj.Hash, writtenHash)
	}
	if alreadyPresent {
		return 0, nil
	}
	return 1, nil
}

func referencedHashes(objType object.ObjectType, data []byte) ([]object.Hash, error) {
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

func uniqueHashes(in []object.Hash) []object.Hash {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[object.Hash]struct{}, len(in))
	out := make([]object.Hash, 0, len(in))
	for _, h := range in {
		h = object.Hash(strings.TrimSpace(string(h)))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

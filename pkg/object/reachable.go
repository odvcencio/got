package object

import (
	"fmt"
	"sort"
	"strings"
)

// ReachableSet returns all object hashes reachable from roots by following
// object references. Missing roots are ignored.
func (s *Store) ReachableSet(roots []Hash) (map[Hash]struct{}, error) {
	roots = uniqueNormalizedHashes(roots)
	out := make(map[Hash]struct{}, len(roots))
	if len(roots) == 0 {
		return out, nil
	}

	stack := make([]Hash, 0, len(roots))
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
		if !s.Has(h) {
			continue
		}
		out[h] = struct{}{}

		objType, data, err := s.Read(h)
		if err != nil {
			return nil, fmt.Errorf("reachable set read %s: %w", h, err)
		}
		refs, err := referencedHashes(objType, data)
		if err != nil {
			return nil, fmt.Errorf("reachable set parse %s (%s): %w", h, objType, err)
		}
		stack = append(stack, refs...)
	}

	return out, nil
}

func referencedHashes(objType ObjectType, data []byte) ([]Hash, error) {
	switch objType {
	case TypeBlob, TypeEntity:
		return nil, nil
	case TypeTag:
		tag, err := UnmarshalTag(data)
		if err != nil {
			return nil, err
		}
		return []Hash{tag.TargetHash}, nil
	case TypeCommit:
		commit, err := UnmarshalCommit(data)
		if err != nil {
			return nil, err
		}
		refs := make([]Hash, 0, 1+len(commit.Parents))
		refs = append(refs, commit.TreeHash)
		refs = append(refs, commit.Parents...)
		return refs, nil
	case TypeTree:
		tree, err := UnmarshalTree(data)
		if err != nil {
			return nil, err
		}
		refs := make([]Hash, 0, len(tree.Entries)*2)
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
	case TypeEntityList:
		el, err := UnmarshalEntityList(data)
		if err != nil {
			return nil, err
		}
		refs := make([]Hash, 0, len(el.EntityRefs))
		refs = append(refs, el.EntityRefs...)
		return refs, nil
	default:
		return nil, fmt.Errorf("unsupported object type %q", objType)
	}
}

func uniqueNormalizedHashes(in []Hash) []Hash {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[Hash]struct{}, len(in))
	out := make([]Hash, 0, len(in))
	for _, h := range in {
		h = Hash(strings.TrimSpace(string(h)))
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

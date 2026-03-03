package repo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
)

// EntitySearchResult represents a single entity match from SearchEntities.
type EntitySearchResult struct {
	Path     string
	Name     string
	Kind     string
	DeclKind string
	Key      string
}

// EntitySearchOptions configures the entity search.
type EntitySearchOptions struct {
	CaseInsensitive bool
	KindFilter      string // filter by entity Kind (e.g. "declaration", "import_block")
	PathPattern     string // glob filter on file path
}

// SearchEntities searches committed entities for names matching the given
// regex pattern. It reads from the committed state (object store), resolving
// HEAD to walk the tree and inspect entity objects.
func (r *Repo) SearchEntities(pattern string, opts EntitySearchOptions) ([]EntitySearchResult, error) {
	if pattern == "" {
		return nil, fmt.Errorf("search entities: pattern must not be empty")
	}

	// Compile regex.
	rePattern := pattern
	if opts.CaseInsensitive {
		rePattern = "(?i)" + rePattern
	}
	re, err := regexp.Compile(rePattern)
	if err != nil {
		return nil, fmt.Errorf("search entities: invalid pattern %q: %w", pattern, err)
	}

	// Resolve HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("search entities: resolve HEAD: %w", err)
	}

	// Read commit.
	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("search entities: read commit: %w", err)
	}

	// Flatten tree.
	entries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("search entities: flatten tree: %w", err)
	}

	var results []EntitySearchResult

	for _, entry := range entries {
		// Skip entries without entity lists.
		if entry.EntityListHash == "" {
			continue
		}

		// Apply path filter.
		if opts.PathPattern != "" {
			matched, err := filepath.Match(opts.PathPattern, entry.Path)
			if err != nil {
				return nil, fmt.Errorf("search entities: invalid path pattern %q: %w", opts.PathPattern, err)
			}
			if !matched {
				// Also try matching against the base name.
				matched, _ = filepath.Match(opts.PathPattern, filepath.Base(entry.Path))
			}
			if !matched {
				continue
			}
		}

		// Read entity list.
		el, err := r.Store.ReadEntityList(entry.EntityListHash)
		if err != nil {
			return nil, fmt.Errorf("search entities: read entity list %s: %w", entry.EntityListHash, err)
		}

		for _, ref := range el.EntityRefs {
			ent, err := r.Store.ReadEntity(ref)
			if err != nil {
				return nil, fmt.Errorf("search entities: read entity %s: %w", ref, err)
			}

			// Apply kind filter.
			if opts.KindFilter != "" && ent.Kind != opts.KindFilter {
				continue
			}

			// Match name against regex.
			if !re.MatchString(ent.Name) {
				continue
			}

			key := ent.Kind + ":" + ent.Name
			results = append(results, EntitySearchResult{
				Path:     entry.Path,
				Name:     ent.Name,
				Kind:     ent.Kind,
				DeclKind: ent.DeclKind,
				Key:      key,
			})
		}
	}

	// Sort by path then name.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].Name < results[j].Name
	})

	return results, nil
}

package repo

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ShortlogEntry summarises commits made by a single author.
type ShortlogEntry struct {
	Author string
	Count  int
	Titles []string // first line of each commit message
}

// ShortlogOptions configures the shortlog walk.
type ShortlogOptions struct {
	Summary  bool // -s: only show counts
	Numbered bool // -n: sort by count descending
	Limit    int  // max commits to walk (0 = all)
}

// Shortlog walks HEAD history (first-parent) and groups commits by author.
// By default entries are sorted by author name; with Numbered they are sorted
// by count descending.
func (r *Repo) Shortlog(opts ShortlogOptions) ([]ShortlogEntry, error) {
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("shortlog: %w", err)
	}

	type authorData struct {
		titles []string
	}
	byAuthor := make(map[string]*authorData)

	current := headHash
	walked := 0
	for current != "" {
		if opts.Limit > 0 && walked >= opts.Limit {
			break
		}

		c, err := r.Store.ReadCommit(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return nil, fmt.Errorf("shortlog: read commit %s: %w", current, err)
		}

		author := c.Author
		title := commitTitle(c.Message)

		ad, ok := byAuthor[author]
		if !ok {
			ad = &authorData{}
			byAuthor[author] = ad
		}
		ad.titles = append(ad.titles, title)

		walked++

		if len(c.Parents) == 0 {
			break
		}
		current = c.Parents[0]
	}

	entries := make([]ShortlogEntry, 0, len(byAuthor))
	for author, ad := range byAuthor {
		entries = append(entries, ShortlogEntry{
			Author: author,
			Count:  len(ad.titles),
			Titles: ad.titles,
		})
	}

	if opts.Numbered {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Count != entries[j].Count {
				return entries[i].Count > entries[j].Count
			}
			return entries[i].Author < entries[j].Author
		})
	} else {
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Author < entries[j].Author
		})
	}

	return entries, nil
}

// commitTitle returns the first line of s (up to the first newline, or all of
// s if there is no newline).
func commitTitle(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// ResolveTreeish resolves a treeish string to a commit hash. It tries, in
// order: refs/tags/<treeish>, refs/heads/<treeish>, HEAD (if treeish is
// "HEAD"), and finally treats the value as a raw hash.
func (r *Repo) ResolveTreeish(treeish string) (object.Hash, error) {
	// Try tag ref first.
	if h, err := r.ResolveRef("refs/tags/" + treeish); err == nil {
		return h, nil
	}
	// Try branch ref.
	if h, err := r.ResolveRef("refs/heads/" + treeish); err == nil {
		return h, nil
	}
	// Try as-is (covers HEAD and full ref paths).
	if h, err := r.ResolveRef(treeish); err == nil {
		return h, nil
	}
	// Treat as raw hash: verify the commit exists.
	h := object.Hash(treeish)
	if _, err := r.Store.ReadCommit(h); err == nil {
		return h, nil
	}
	return "", fmt.Errorf("cannot resolve treeish %q", treeish)
}

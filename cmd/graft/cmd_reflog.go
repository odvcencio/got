package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newReflogCmd() *cobra.Command {
	var limit int
	var entityFilter string

	cmd := &cobra.Command{
		Use:   "reflog [ref]",
		Short: "Show ref update history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}

			// When --entity is provided, use entity-aware reader and filter.
			if strings.TrimSpace(entityFilter) != "" {
				return runReflogEntity(cmd, r, ref, entityFilter, limit)
			}

			entries, err := r.ReadReflog(ref, limit)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, e := range entries {
				sha := string(e.NewHash)
				if len(sha) > 8 {
					sha = sha[:8]
				}
				ts := time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339)
				fmt.Fprintf(out, "%s %s %s %s\n", sha, ts, e.Ref, e.Reason)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum entries to show")
	cmd.Flags().StringVar(&entityFilter, "entity", "", "filter entries by entity key (e.g., func:Handler, type:Config*)")
	return cmd
}

// runReflogEntity reads entity-enriched reflog entries and filters them to
// show only entries that contain changes matching the given entity filter.
// The filter is matched against each entry's EntityKey using glob matching
// on the name portion and exact matching on the kind prefix.
func runReflogEntity(cmd *cobra.Command, r *repo.Repo, ref, entityFilter string, limit int) error {
	// Read all entries (we filter client-side, so request a generous limit).
	allEntries, err := r.ReadReflogWithEntities(ref, 0)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	shown := 0

	for _, e := range allEntries {
		if limit > 0 && shown >= limit {
			break
		}

		// Find matching entity changes in this entry.
		var matched []repo.ReflogEntityChange
		for _, ec := range e.Entities {
			if matchEntityFilter(entityFilter, ec.EntityKey) {
				matched = append(matched, ec)
			}
		}

		if len(matched) == 0 {
			continue
		}

		sha := string(e.NewHash)
		if len(sha) > 8 {
			sha = sha[:8]
		}
		ts := time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339)
		fmt.Fprintf(out, "%s %s %s %s\n", sha, ts, e.Ref, e.Reason)
		for _, ec := range matched {
			fmt.Fprintf(out, "  %s %s [%s]\n", ec.Path, ec.EntityKey, ec.ChangeType)
		}
		shown++
	}

	return nil
}

// matchEntityFilter checks if an entity key matches the given filter pattern.
// The filter has the form "kind:nameGlob" (e.g., "func:*Handler").
// If the filter contains no colon, it is matched against the full entity key.
func matchEntityFilter(filter, entityKey string) bool {
	// If filter has a colon, split and match kind exactly + name with glob.
	if fColon := strings.Index(filter, ":"); fColon >= 0 {
		fKind := filter[:fColon]
		fName := filter[fColon+1:]

		eColon := strings.Index(entityKey, ":")
		if eColon < 0 {
			return false
		}
		eKind := entityKey[:eColon]
		eName := entityKey[eColon+1:]

		if fKind != eKind {
			return false
		}

		// Use simple glob matching for the name portion.
		return simpleGlobMatch(fName, eName)
	}

	// No colon in filter: match against the full entity key as glob.
	return simpleGlobMatch(filter, entityKey)
}

// simpleGlobMatch performs basic glob matching supporting * and ? wildcards.
// * matches any number of non-separator characters, ? matches exactly one.
func simpleGlobMatch(pattern, str string) bool {
	// Use strings-based matching to avoid filepath separator issues.
	// This is a simple recursive implementation.
	return globMatch(pattern, str)
}

func globMatch(pattern, str string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Skip consecutive stars.
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			// Try matching the rest of the pattern at every position.
			for i := 0; i <= len(str); i++ {
				if globMatch(pattern, str[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(str) == 0 {
				return false
			}
			pattern = pattern[1:]
			str = str[1:]
		default:
			if len(str) == 0 || pattern[0] != str[0] {
				return false
			}
			pattern = pattern[1:]
			str = str[1:]
		}
	}
	return len(str) == 0
}

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [commit-ish]",
		Short: "Show commit metadata and changed files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			target := "HEAD"
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				target = strings.TrimSpace(args[0])
			}

			h, err := resolveCommitish(r, target)
			if err != nil {
				return err
			}
			commit, err := r.Store.ReadCommit(h)
			if err != nil {
				return fmt.Errorf("show: read commit %s: %w", h, err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "commit %s\n", h)
			fmt.Fprintf(out, "Author: %s\n", commit.Author)
			fmt.Fprintf(out, "Date:   %s\n", time.Unix(commit.Timestamp, 0).Format("2006-01-02 15:04:05"))
			fmt.Fprintln(out)
			fmt.Fprintf(out, "    %s\n", commit.Message)
			fmt.Fprintln(out)

			before := make(map[string]repo.TreeFileEntry)
			if len(commit.Parents) > 0 {
				parent, err := r.Store.ReadCommit(commit.Parents[0])
				if err == nil {
					if parentEntries, flattenErr := r.FlattenTree(parent.TreeHash); flattenErr == nil {
						for _, e := range parentEntries {
							before[e.Path] = e
						}
					}
				}
			}

			after := make(map[string]repo.TreeFileEntry)
			afterEntries, err := r.FlattenTree(commit.TreeHash)
			if err != nil {
				return fmt.Errorf("show: flatten tree: %w", err)
			}
			for _, e := range afterEntries {
				after[e.Path] = e
			}

			changes := summarizeTreeChanges(before, after)
			if len(changes) == 0 {
				return nil
			}

			fmt.Fprintln(out, "Changes:")
			for _, line := range changes {
				fmt.Fprintf(out, "  %s\n", line)
			}
			return nil
		},
	}
}

func resolveCommitish(r *repo.Repo, target string) (object.Hash, error) {
	if resolved, err := r.ResolveRef(target); err == nil {
		return resolved, nil
	}
	h := object.Hash(filepath.ToSlash(strings.TrimSpace(target)))
	if h == "" {
		return "", fmt.Errorf("show: empty commit-ish")
	}
	if _, err := r.Store.ReadCommit(h); err != nil {
		return "", fmt.Errorf("show: unknown ref or commit %q", target)
	}
	return h, nil
}

func summarizeTreeChanges(before, after map[string]repo.TreeFileEntry) []string {
	paths := make(map[string]struct{}, len(before)+len(after))
	for p := range before {
		paths[p] = struct{}{}
	}
	for p := range after {
		paths[p] = struct{}{}
	}

	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	out := make([]string, 0, len(sorted))
	for _, p := range sorted {
		b, inBefore := before[p]
		a, inAfter := after[p]
		switch {
		case !inBefore && inAfter:
			out = append(out, "A "+p)
		case inBefore && !inAfter:
			out = append(out, "D "+p)
		case inBefore && inAfter && (b.BlobHash != a.BlobHash || b.Mode != a.Mode):
			out = append(out, "M "+p)
		}
	}
	return out
}

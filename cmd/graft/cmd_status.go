package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show working tree status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			entries, err := r.Status()
			if err != nil {
				return err
			}

			// Determine current branch and whether commits exist.
			branch := "main"
			noCommits := true

			head, err := r.Head()
			if err == nil {
				if strings.HasPrefix(head, "refs/heads/") {
					branch = strings.TrimPrefix(head, "refs/heads/")
				}
				// Check if the ref actually resolves to a commit.
				if _, resolveErr := r.ResolveRef("HEAD"); resolveErr == nil {
					noCommits = false
				}
			}

			if jsonFlag {
				return statusJSON(cmd, entries, branch, noCommits)
			}

			out := cmd.OutOrStdout()

			if noCommits {
				fmt.Fprintf(out, "on %s (no commits yet)\n", branch)
			} else {
				fmt.Fprintf(out, "on %s\n", branch)
			}

			// Categorize entries.
			var conflicts, staged, unstaged, untracked []string

			for _, e := range entries {
				if e.IndexStatus == repo.StatusConflict || e.WorkStatus == repo.StatusConflict {
					conflicts = append(conflicts, fmt.Sprintf("  ! %s", filepath.ToSlash(e.Path)))
					continue
				}

				// Staged: changes in index relative to HEAD.
				switch e.IndexStatus {
				case repo.StatusNew:
					staged = append(staged, fmt.Sprintf("  + %s", filepath.ToSlash(e.Path)))
				case repo.StatusModified:
					staged = append(staged, fmt.Sprintf("  ~ %s", filepath.ToSlash(e.Path)))
				case repo.StatusRenamed:
					staged = append(staged, fmt.Sprintf("  R %s -> %s", filepath.ToSlash(e.RenamedFrom), filepath.ToSlash(e.Path)))
				case repo.StatusDeleted:
					staged = append(staged, fmt.Sprintf("  - %s", filepath.ToSlash(e.Path)))
				}

				// Unstaged: changes in working tree relative to index.
				switch e.WorkStatus {
				case repo.StatusDirty:
					unstaged = append(unstaged, fmt.Sprintf("  ~ %s", filepath.ToSlash(e.Path)))
				case repo.StatusRenamed:
					unstaged = append(unstaged, fmt.Sprintf("  R %s -> %s", filepath.ToSlash(e.RenamedFrom), filepath.ToSlash(e.Path)))
				case repo.StatusDeleted:
					// Only show as unstaged deletion if the file is actually staged
					// (not untracked).
					if e.IndexStatus != repo.StatusUntracked {
						unstaged = append(unstaged, fmt.Sprintf("  - %s", filepath.ToSlash(e.Path)))
					}
				}

				// Untracked: not in staging at all.
				if e.IndexStatus == repo.StatusUntracked && e.WorkStatus != repo.StatusRenamed {
					untracked = append(untracked, fmt.Sprintf("  %s", filepath.ToSlash(e.Path)))
				}
			}

			if len(conflicts) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "conflicts:")
				for _, s := range conflicts {
					fmt.Fprintln(out, s)
				}
			}

			if len(staged) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "staged:")
				for _, s := range staged {
					fmt.Fprintln(out, s)
				}
			}

			if len(unstaged) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "unstaged:")
				for _, s := range unstaged {
					fmt.Fprintln(out, s)
				}
			}

			if len(untracked) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "untracked:")
				for _, s := range untracked {
					fmt.Fprintln(out, s)
				}
			}

			if gitbridge.IsBridgeRepo(r.RootDir) {
				b, err := gitbridge.OpenBridge(r.RootDir)
				if err == nil {
					defer b.Close()
					_, err := b.GitHEAD()
					if err == nil {
						// Simple check: just show bridge is active
						fmt.Println("\ngit bridge: active")
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}

// statusJSON builds and writes the JSON output for the status command.
func statusJSON(cmd *cobra.Command, entries []repo.StatusEntry, branch string, noCommits bool) error {
	result := JSONStatusOutput{
		Branch:    branch,
		NoCommits: noCommits,
	}

	for _, e := range entries {
		p := filepath.ToSlash(e.Path)

		if e.IndexStatus == repo.StatusConflict || e.WorkStatus == repo.StatusConflict {
			result.Conflicts = append(result.Conflicts, JSONStatusEntry{
				Path:   p,
				Status: "conflict",
			})
			continue
		}

		// Staged changes.
		switch e.IndexStatus {
		case repo.StatusNew:
			result.Staged = append(result.Staged, JSONStatusEntry{Path: p, Status: "new"})
		case repo.StatusModified:
			result.Staged = append(result.Staged, JSONStatusEntry{Path: p, Status: "modified"})
		case repo.StatusRenamed:
			result.Staged = append(result.Staged, JSONStatusEntry{
				Path:        p,
				Status:      "renamed",
				RenamedFrom: filepath.ToSlash(e.RenamedFrom),
			})
		case repo.StatusDeleted:
			result.Staged = append(result.Staged, JSONStatusEntry{Path: p, Status: "deleted"})
		}

		// Unstaged changes.
		switch e.WorkStatus {
		case repo.StatusDirty:
			result.Unstaged = append(result.Unstaged, JSONStatusEntry{Path: p, Status: "modified"})
		case repo.StatusRenamed:
			result.Unstaged = append(result.Unstaged, JSONStatusEntry{
				Path:        p,
				Status:      "renamed",
				RenamedFrom: filepath.ToSlash(e.RenamedFrom),
			})
		case repo.StatusDeleted:
			if e.IndexStatus != repo.StatusUntracked {
				result.Unstaged = append(result.Unstaged, JSONStatusEntry{Path: p, Status: "deleted"})
			}
		}

		// Untracked files.
		if e.IndexStatus == repo.StatusUntracked && e.WorkStatus != repo.StatusRenamed {
			result.Untracked = append(result.Untracked, p)
		}
	}

	return writeJSON(cmd.OutOrStdout(), result)
}

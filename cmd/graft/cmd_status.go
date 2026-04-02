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
	var shortFlag bool

	cmd := &cobra.Command{
		Use:   "status [-s|--short] [--json]",
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

			if jsonFlag && shortFlag {
				return fmt.Errorf("--json and --short cannot be used together")
			}

			if jsonFlag {
				return statusJSON(cmd, r, entries, branch, noCommits)
			}

			if shortFlag {
				return statusShort(cmd, entries)
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

			if r.HasShadowFailures() {
				fmt.Fprintln(out, "\nwarning: git shadow out of sync (run 'graft repair resync-git' to fix)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVarP(&shortFlag, "short", "s", false, "output in short format")

	return cmd
}

// statusJSON builds and writes the JSON output for the status command.
func statusJSON(cmd *cobra.Command, r *repo.Repo, entries []repo.StatusEntry, branch string, noCommits bool) error {
	result := JSONStatusOutput{
		Branch:       branch,
		NoCommits:    noCommits,
		ShadowDesync: r.HasShadowFailures(),
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

func statusShort(cmd *cobra.Command, entries []repo.StatusEntry) error {
	out := cmd.OutOrStdout()
	for _, entry := range entries {
		line := shortStatusLine(entry)
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func shortStatusLine(entry repo.StatusEntry) string {
	path := filepath.ToSlash(entry.Path)
	if entry.IndexStatus == repo.StatusConflict || entry.WorkStatus == repo.StatusConflict {
		return fmt.Sprintf("UU %s", path)
	}
	if entry.IndexStatus == repo.StatusUntracked && entry.WorkStatus != repo.StatusRenamed {
		return fmt.Sprintf("?? %s", path)
	}

	indexCode := shortIndexStatusCode(entry.IndexStatus)
	workCode := shortWorkStatusCode(entry.IndexStatus, entry.WorkStatus)
	if indexCode == ' ' && workCode == ' ' {
		return ""
	}
	if entry.RenamedFrom != "" && (indexCode == 'R' || workCode == 'R') {
		return fmt.Sprintf("%c%c %s -> %s", indexCode, workCode, filepath.ToSlash(entry.RenamedFrom), path)
	}
	return fmt.Sprintf("%c%c %s", indexCode, workCode, path)
}

func shortIndexStatusCode(status repo.FileStatus) byte {
	switch status {
	case repo.StatusNew:
		return 'A'
	case repo.StatusModified:
		return 'M'
	case repo.StatusRenamed:
		return 'R'
	case repo.StatusDeleted:
		return 'D'
	default:
		return ' '
	}
}

func shortWorkStatusCode(indexStatus, workStatus repo.FileStatus) byte {
	switch workStatus {
	case repo.StatusDirty:
		return 'M'
	case repo.StatusRenamed:
		return 'R'
	case repo.StatusDeleted:
		if indexStatus != repo.StatusUntracked {
			return 'D'
		}
	}
	return ' '
}

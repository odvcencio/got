package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newConflictsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "conflicts",
		Short: "List entity-level conflicts in the working tree",
		Long: `Show all conflicted files and entities after a merge or rebase.

For each conflicted file, lists the specific entities (functions, types, etc.)
that are in conflict and the type of conflict (both modified, delete vs modify).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			conflicts, err := r.ListConflicts()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if len(conflicts) == 0 {
				fmt.Fprintln(out, "no conflicts")
				return nil
			}

			// Group conflicts by path for readable output.
			var currentPath string
			for _, c := range conflicts {
				if c.Path != currentPath {
					currentPath = c.Path
					fmt.Fprintf(out, "%s\n", c.Path)
				}
				if c.EntityName != "" {
					fmt.Fprintf(out, "    %s: %s\n", c.EntityName, formatConflictType(c.ConflictType))
				} else {
					fmt.Fprintf(out, "    (text conflict)\n")
				}
			}

			return nil
		},
	}
}

// formatConflictType returns a human-readable label for a conflict type.
func formatConflictType(ct string) string {
	switch ct {
	case "both_modified":
		return "both modified"
	case "delete_vs_modify":
		return "delete vs modify"
	default:
		return ct
	}
}

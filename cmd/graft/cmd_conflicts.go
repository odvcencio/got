package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newConflictsCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
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

			if jsonFlag {
				return conflictsJSON(cmd, conflicts)
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
					fmt.Fprintf(out, "    %s: %s\n", c.EntityName, humanConflictType(c.ConflictType))
				} else {
					fmt.Fprintf(out, "    (text conflict)\n")
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}

// conflictsJSON groups conflict entries by file path and writes JSON output.
func conflictsJSON(cmd *cobra.Command, conflicts []repo.ConflictEntry) error {
	// Group by path, maintaining order.
	fileMap := make(map[string]*JSONConflictFile)
	var fileOrder []string

	for _, c := range conflicts {
		jf, ok := fileMap[c.Path]
		if !ok {
			jf = &JSONConflictFile{Path: c.Path}
			fileMap[c.Path] = jf
			fileOrder = append(fileOrder, c.Path)
		}
		jf.Entities = append(jf.Entities, JSONConflictEntity{
			EntityName:   c.EntityName,
			EntityKey:    c.EntityKey,
			EntityKind:   c.EntityKind,
			ConflictType: c.ConflictType,
		})
	}

	result := JSONConflictsOutput{
		Files: make([]JSONConflictFile, 0, len(fileOrder)),
	}
	for _, path := range fileOrder {
		result.Files = append(result.Files, *fileMap[path])
	}

	return writeJSON(cmd.OutOrStdout(), result)
}

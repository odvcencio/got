package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newTagCmd() *cobra.Command {
	var deleteTag string
	var force bool
	var showHash bool

	cmd := &cobra.Command{
		Use:   "tag [name] [target]",
		Short: "List, create, or delete lightweight tags",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if strings.TrimSpace(deleteTag) != "" {
				if len(args) > 0 {
					return fmt.Errorf("tag --delete does not accept positional args")
				}
				return r.DeleteTag(deleteTag)
			}

			if len(args) == 0 {
				tags, err := r.ListTagsWithHashes()
				if err != nil {
					return err
				}
				names := make([]string, 0, len(tags))
				for name := range tags {
					names = append(names, name)
				}
				sort.Strings(names)

				for _, name := range names {
					if showHash {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", tags[name], name)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), name)
					}
				}
				return nil
			}

			name := args[0]
			var target object.Hash
			if len(args) == 2 {
				targetArg := strings.TrimSpace(args[1])
				if resolved, err := r.ResolveRef(targetArg); err == nil {
					target = resolved
				} else {
					target = object.Hash(targetArg)
				}
			} else {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("resolve HEAD: %w", err)
				}
				target = head
			}

			return r.CreateTag(name, target, force)
		},
	}

	cmd.Flags().StringVarP(&deleteTag, "delete", "d", "", "delete the named tag")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace an existing tag")
	cmd.Flags().BoolVar(&showHash, "show-hash", false, "show tag target hashes when listing")

	return cmd
}

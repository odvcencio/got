package main

import (
	"fmt"
	"sort"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage repository remotes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			cfg, err := r.ReadConfig()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(cfg.Remotes))
			for name := range cfg.Remotes {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, cfg.Remotes[name])
			}
			return nil
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a named remote",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			remoteURL, _, err := parseAnyRemoteSpec(args[1])
			if err != nil {
				return fmt.Errorf("invalid remote URL %q: %w", args[1], err)
			}
			if err := r.SetRemote(args[0], remoteURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added remote %q -> %s\n", args[0], remoteURL)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set-url <name> <url>",
		Short: "Update a named remote URL",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			remoteURL, _, err := parseAnyRemoteSpec(args[1])
			if err != nil {
				return fmt.Errorf("invalid remote URL %q: %w", args[1], err)
			}
			if err := r.SetRemote(args[0], remoteURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated remote %q -> %s\n", args[0], remoteURL)
			return nil
		},
	})

	return cmd
}

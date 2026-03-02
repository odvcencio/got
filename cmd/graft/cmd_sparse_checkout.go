package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newSparseCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sparse-checkout",
		Short: "Manage sparse checkout patterns",
	}

	cmd.AddCommand(newSparseCheckoutSetCmd())
	cmd.AddCommand(newSparseCheckoutAddCmd())
	cmd.AddCommand(newSparseCheckoutListCmd())
	cmd.AddCommand(newSparseCheckoutDisableCmd())

	return cmd
}

func newSparseCheckoutSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <patterns...>",
		Short: "Set sparse checkout patterns (replaces existing)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.SparseCheckoutSet(args); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Sparse checkout patterns set (%d patterns).\n", len(args))
			return nil
		},
	}
}

func newSparseCheckoutAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <patterns...>",
		Short: "Add patterns to sparse checkout",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.SparseCheckoutAdd(args); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %d pattern(s) to sparse checkout.\n", len(args))
			return nil
		},
	}
}

func newSparseCheckoutListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List current sparse checkout patterns",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			patterns, err := r.SparseCheckoutList()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, p := range patterns {
				fmt.Fprintln(out, p)
			}
			return nil
		},
	}
}

func newSparseCheckoutDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable sparse checkout and materialize all files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.SparseCheckoutDisable(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Sparse checkout disabled.")
			return nil
		},
	}
}

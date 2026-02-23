package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "got",
		Short: "Structural version control powered by tree-sitter",
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newAddCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newCommitCmd())
	root.AddCommand(newLogCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newMergeCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("got 0.1.0-dev")
		},
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.2.3-dev"

func main() {
	root := &cobra.Command{
		Use:   "got",
		Short: "Structural version control powered by tree-sitter",
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newAddCmd())
	root.AddCommand(newResetCmd())
	root.AddCommand(newRmCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newCommitCmd())
	root.AddCommand(newLogCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newBlameCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newTagCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newCherryPickCmd())
	root.AddCommand(newRemoteCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newCloneCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newReflogCmd())
	root.AddCommand(newGcCmd())
	root.AddCommand(newVerifyCmd())

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
			fmt.Println("got " + version)
		},
	}
}

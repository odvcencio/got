// Package main implements the graft CLI, a structural version control system.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.7.0"

func main() {
	root := &cobra.Command{
		Use:   "graft",
		Short: "Structural version control powered by tree-sitter",
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newAddCmd())
	root.AddCommand(newResetCmd())
	root.AddCommand(newRmCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newCheckIgnoreCmd())
	root.AddCommand(newCommitCmd())
	root.AddCommand(newLogCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newBlameCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newTagCmd())
	root.AddCommand(newCheckoutCmd())
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newConflictsCmd())
	root.AddCommand(newCherryPickCmd())
	root.AddCommand(newRevertCmd())
	root.AddCommand(newRemoteCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newCloneCmd())
	root.AddCommand(newFetchCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newReflogCmd())
	root.AddCommand(newGcCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newStashCmd())
	root.AddCommand(newRebaseCmd())
	root.AddCommand(newSparseCheckoutCmd())
	root.AddCommand(newLFSCmd())
	root.AddCommand(newBisectCmd())
	root.AddCommand(newWorktreeCmd())
	root.AddCommand(newCleanCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newShortlogCmd())
	root.AddCommand(newArchiveCmd())
	root.AddCommand(newModuleCmd())
	root.AddCommand(newRepairCmd())
	root.AddCommand(newWorkonCmd())
	root.AddCommand(newCoordCmd())
	root.AddCommand(newCoorddCmd())
	root.AddCommand(newWorkspaceCmd())
	root.AddCommand(newMCPCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var exitCoder interface{ ExitCode() int }
		if errors.As(err, &exitCoder) {
			os.Exit(exitCoder.ExitCode())
		}
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("graft " + version)
		},
	}
}

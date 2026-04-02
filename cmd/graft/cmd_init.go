package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var noGit bool
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Create an empty graft repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Ensure the target directory exists.
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

			// Existing .git/ → bridge init as today (no behavior change)
			if gitbridge.DetectGitRepo(abs) {
				bridge, err := gitbridge.InitBridge(abs)
				if err != nil {
					return fmt.Errorf("init git bridge: %w", err)
				}
				bridge.Close()
				excludeFromGitInfoExclude(abs, ".gts/")
				fmt.Println("Initialized graft bridge alongside existing git repository")
				return nil
			}

			// Fresh directory: create .graft/ first
			r, err := repo.Init(abs)
			if err != nil {
				return err
			}

			// Then optionally create .git/ and bridge
			if !noGit {
				_ = repo.RunExternalProcess(repo.ExternalProcessSpec{
					Dir: abs, Path: "git", Args: []string{"init"},
					Stdout: io.Discard, Stderr: io.Discard, Label: "init-git",
				})
				if gitbridge.DetectGitRepo(abs) {
					bridge, _ := gitbridge.InitBridge(abs)
					if bridge != nil {
						bridge.Close()
					}
					excludeFromGitInfoExclude(abs, ".gts/")
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "initialized graft repository in %s\n",
				filepath.Join(r.RootDir, ".graft")+string(filepath.Separator))
			return nil
		},
	}
	cmd.Flags().BoolVar(&noGit, "no-git", false, "skip creating .git/ directory")
	return cmd
}

func excludeFromGitInfoExclude(repoRoot, pattern string) {
	excludePath := filepath.Join(repoRoot, ".git", "info", "exclude")
	data, _ := os.ReadFile(excludePath)
	if strings.Contains(string(data), pattern) {
		return
	}
	os.MkdirAll(filepath.Dir(excludePath), 0o755)
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(pattern + "\n")
}

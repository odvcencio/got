package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newBisectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bisect",
		Short: "Binary search to find the commit that introduced a bug",
	}

	cmd.AddCommand(newBisectStartCmd())
	cmd.AddCommand(newBisectGoodCmd())
	cmd.AddCommand(newBisectBadCmd())
	cmd.AddCommand(newBisectSkipCmd())
	cmd.AddCommand(newBisectResetCmd())
	cmd.AddCommand(newBisectLogCmd())
	cmd.AddCommand(newBisectRunCmd())

	return cmd
}

func newBisectStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <bad> <good>",
		Short: "Start a bisect session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			bad, err := resolveTarget(r, args[0])
			if err != nil {
				return fmt.Errorf("bisect start: %w", err)
			}
			good, err := resolveTarget(r, args[1])
			if err != nil {
				return fmt.Errorf("bisect start: %w", err)
			}

			result, err := r.BisectStart(bad, good)
			if err != nil {
				return err
			}

			printBisectResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
}

func newBisectGoodCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "good",
		Short: "Mark current commit as good",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			result, err := r.BisectGood()
			if err != nil {
				return err
			}

			printBisectResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
}

func newBisectBadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bad",
		Short: "Mark current commit as bad",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			result, err := r.BisectBad()
			if err != nil {
				return err
			}

			printBisectResult(cmd.OutOrStdout(), result)
			return nil
		},
	}
}

func newBisectSkipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skip",
		Short: "Skip the current commit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			head, err := r.ResolveRef("HEAD")
			if err != nil {
				return fmt.Errorf("bisect skip: resolve HEAD: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Skipping %s...\n", shortHash(head))

			result, err := r.BisectSkip()
			if err != nil {
				return err
			}

			printBisectResult(out, result)
			return nil
		},
	}
}

func newBisectResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "End bisect session and restore original ref",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Read start-ref before reset cleans up the state.
			startRef := readBisectStartRef(r)

			if err := r.BisectReset(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Bisect reset. Restored to %s.\n", startRef)
			return nil
		},
	}
}

func newBisectLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log",
		Short: "Print bisect log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			lines, err := r.BisectLog()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, line := range lines {
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
}

func newBisectRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <script>",
		Short: "Automated bisect using a script (exit 0 = good, non-zero = bad)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			script := args[0]

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if !r.IsBisecting() {
				return fmt.Errorf("bisect run: not bisecting (use bisect start first)")
			}

			out := cmd.OutOrStdout()

			for {
				// Run the user script.
				c := exec.Command("sh", "-c", script)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				runErr := c.Run()

				var result *repo.BisectResult
				if runErr == nil {
					// Exit 0 means good.
					result, err = r.BisectGood()
				} else {
					// Non-zero exit means bad.
					result, err = r.BisectBad()
				}
				if err != nil {
					return err
				}

				printBisectResult(out, result)

				if result.Done {
					return nil
				}
			}
		},
	}
}

// printBisectResult prints the result of a bisect step.
func printBisectResult(out io.Writer, result *repo.BisectResult) {
	if result.Done {
		fmt.Fprintf(out, "%s is the first bad commit\n%s\n", result.FirstBad, result.Message)
		return
	}
	fmt.Fprintf(out, "Bisecting: %d revisions left to test (roughly %d steps)\n", result.Remaining, result.Steps)
	fmt.Fprintf(out, "[%s] %s\n", shortHash(result.Current), result.Message)
}

// readBisectStartRef reads the original HEAD ref from bisect state, before
// reset cleans it up. Returns the raw string (branch name or hash).
func readBisectStartRef(r *repo.Repo) string {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "bisect", "start-ref"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newLFSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lfs",
		Short: "Manage large file storage",
	}

	cmd.AddCommand(newLFSTrackCmd())
	cmd.AddCommand(newLFSUntrackCmd())
	cmd.AddCommand(newLFSLsFilesCmd())
	cmd.AddCommand(newLFSStatusCmd())

	return cmd
}

func newLFSTrackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "track <pattern>",
		Short: "Track files matching pattern with LFS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			pattern := args[0]
			attrPath := filepath.Join(r.RootDir, ".graftattributes")
			line := pattern + " filter=lfs diff=lfs merge=lfs"

			// Read existing file to check for duplicates.
			existing, err := os.ReadFile(attrPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("lfs track: read .graftattributes: %w", err)
			}

			if len(existing) > 0 {
				scanner := bufio.NewScanner(strings.NewReader(string(existing)))
				for scanner.Scan() {
					fields := strings.Fields(scanner.Text())
					if len(fields) > 0 && fields[0] == pattern {
						fmt.Fprintf(cmd.OutOrStdout(), "Pattern %q is already tracked\n", pattern)
						return nil
					}
				}
			}

			// Append the new line.
			f, err := os.OpenFile(attrPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("lfs track: open .graftattributes: %w", err)
			}
			defer f.Close()

			if _, err := fmt.Fprintln(f, line); err != nil {
				return fmt.Errorf("lfs track: write .graftattributes: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Tracking %q\n", pattern)
			return nil
		},
	}
}

func newLFSUntrackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "untrack <pattern>",
		Short: "Stop tracking files matching pattern with LFS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			pattern := args[0]
			attrPath := filepath.Join(r.RootDir, ".graftattributes")

			data, err := os.ReadFile(attrPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("lfs untrack: .graftattributes does not exist")
				}
				return fmt.Errorf("lfs untrack: read .graftattributes: %w", err)
			}

			var kept []string
			found := false
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := scanner.Text()
				fields := strings.Fields(line)
				if len(fields) > 0 && fields[0] == pattern {
					found = true
					continue
				}
				kept = append(kept, line)
			}

			if !found {
				return fmt.Errorf("lfs untrack: pattern %q not found in .graftattributes", pattern)
			}

			content := strings.Join(kept, "\n")
			if len(kept) > 0 {
				content += "\n"
			}
			if err := os.WriteFile(attrPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("lfs untrack: write .graftattributes: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Untracking %q\n", pattern)
			return nil
		},
	}
}

func newLFSLsFilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls-files",
		Short: "List LFS-tracked files in staging",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			statuses, err := r.LFSStatus()
			if err != nil {
				return err
			}

			// Sort by path for deterministic output.
			sort.Slice(statuses, func(i, j int) bool {
				return statuses[i].Path < statuses[j].Path
			})

			out := cmd.OutOrStdout()
			for _, s := range statuses {
				shortOID := s.OID
				if len(shortOID) > 12 {
					shortOID = shortOID[:12]
				}
				indicator := "*" // content missing
				if s.HasContent {
					indicator = "-" // content present
				}
				fmt.Fprintf(out, "%s %s %s\n", shortOID, indicator, s.Path)
			}
			return nil
		},
	}
}

func newLFSStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show LFS status for tracked files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			statuses, err := r.LFSStatus()
			if err != nil {
				return err
			}

			// Sort by path for deterministic output.
			sort.Slice(statuses, func(i, j int) bool {
				return statuses[i].Path < statuses[j].Path
			})

			out := cmd.OutOrStdout()
			if len(statuses) == 0 {
				fmt.Fprintln(out, "No LFS objects found in staging")
				return nil
			}

			fmt.Fprintln(out, "LFS objects:")
			for _, s := range statuses {
				shortOID := s.OID
				if len(shortOID) > 12 {
					shortOID = shortOID[:12]
				}
				contentStatus := "missing"
				if s.HasContent {
					contentStatus = "present"
				}
				fmt.Fprintf(out, "  %s  %s (oid: %s, size: %d, content: %s)\n",
					s.Path, shortOID, s.OID, s.Size, contentStatus)
			}
			return nil
		},
	}
}

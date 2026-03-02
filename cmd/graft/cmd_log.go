package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	var oneline bool
	var limit int
	var entitySelector string
	var all bool
	var graph bool

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show commit history",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Determine the current branch name for decoration.
			branchName := ""
			head, err := r.Head()
			if err == nil && strings.HasPrefix(head, "refs/heads/") {
				branchName = strings.TrimPrefix(head, "refs/heads/")
			}

			headHash, err := r.ResolveRef("HEAD")
			if err != nil {
				return fmt.Errorf("cannot resolve HEAD: %w", err)
			}

			// Collect ref decorations when --all is used.
			var refDecorations map[object.Hash][]string
			if all {
				refDecorations, err = buildRefDecorations(r)
				if err != nil {
					return err
				}
			}

			if strings.TrimSpace(entitySelector) != "" {
				selector, err := parseLogEntitySelector(entitySelector)
				if err != nil {
					return err
				}

				entries, err := r.LogByEntity(headHash, limit, selector.Path, selector.Key)
				if err != nil {
					return err
				}
				if len(entries) == 0 {
					return nil
				}

				out := cmd.OutOrStdout()
				for _, entry := range entries {
					h := entry.Hash
					c := entry.Commit
					decoration := buildDecoration(h, headHash, branchName)

					if oneline {
						short := shortHash(h)
						if decoration != "" {
							fmt.Fprintf(out, "%s %s %s\n", short, decoration, c.Message)
						} else {
							fmt.Fprintf(out, "%s %s\n", short, c.Message)
						}
					} else {
						if decoration != "" {
							fmt.Fprintf(out, "commit %s %s\n", h, decoration)
						} else {
							fmt.Fprintf(out, "commit %s\n", h)
						}
						fmt.Fprintf(out, "Author: %s\n", c.Author)
						fmt.Fprintf(out, "Date:   %s\n", time.Unix(c.Timestamp, 0).Format("2006-01-02 15:04:05"))
						fmt.Fprintln(out)
						fmt.Fprintf(out, "    %s\n", c.Message)
						fmt.Fprintln(out)
					}
				}
				return nil
			}

			var entries []repo.LogEntry

			if all {
				entries, err = r.LogAll(limit)
				if err != nil {
					return err
				}
			} else {
				commits, err := r.Log(headHash, limit)
				if err != nil {
					return err
				}

				// Convert to LogEntry slice with hashes.
				if len(commits) > 0 {
					hashes := make([]object.Hash, len(commits))
					hashes[0] = headHash
					for i := 1; i < len(commits); i++ {
						hashes[i] = commits[i-1].Parents[0]
					}
					for i, c := range commits {
						entries = append(entries, repo.LogEntry{Hash: hashes[i], Commit: c})
					}
				}
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no commits yet")
				return nil
			}

			out := cmd.OutOrStdout()

			var graphLines []string
			if graph {
				graphLines = renderGraph(entries)
			}

			for i, entry := range entries {
				h := entry.Hash
				c := entry.Commit

				var decoration string
				if all && refDecorations != nil {
					decoration = buildAllDecoration(h, headHash, branchName, refDecorations)
				} else {
					decoration = buildDecoration(h, headHash, branchName)
				}

				graphPrefix := ""
				if graph && i < len(graphLines) {
					graphPrefix = graphLines[i]
				}

				if oneline {
					short := shortHash(h)
					line := short
					if decoration != "" {
						line += " " + decoration
					}
					line += " " + c.Message
					if graphPrefix != "" {
						fmt.Fprintf(out, "%s %s\n", graphPrefix, line)
					} else {
						fmt.Fprintln(out, line)
					}
				} else {
					commitLine := "commit " + string(h)
					if decoration != "" {
						commitLine += " " + decoration
					}
					if graphPrefix != "" {
						fmt.Fprintf(out, "%s %s\n", graphPrefix, commitLine)
					} else {
						fmt.Fprintln(out, commitLine)
					}
					fmt.Fprintf(out, "Author: %s\n", c.Author)
					fmt.Fprintf(out, "Date:   %s\n", time.Unix(c.Timestamp, 0).Format("2006-01-02 15:04:05"))
					fmt.Fprintln(out)
					fmt.Fprintf(out, "    %s\n", c.Message)
					fmt.Fprintln(out)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&oneline, "oneline", false, "compact one-line format")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "maximum number of commits to show")
	cmd.Flags().StringVar(&entitySelector, "entity", "", "filter commits by entity selector (path::entity_key or entity_key)")
	cmd.Flags().BoolVar(&all, "all", false, "show commits from all branches and tags")
	cmd.Flags().BoolVar(&graph, "graph", false, "draw an ASCII commit graph alongside the log")

	return cmd
}

// buildDecoration returns a string like "(HEAD -> main)" if the commit is
// the current HEAD, or "" otherwise.
func buildDecoration(commitHash, headHash object.Hash, branchName string) string {
	if commitHash != headHash {
		return ""
	}
	if branchName != "" {
		return "(HEAD -> " + branchName + ")"
	}
	return "(HEAD)"
}

// buildAllDecoration returns decoration for --all mode, showing all refs
// that point to this commit.
func buildAllDecoration(commitHash, headHash object.Hash, branchName string, refDecorations map[object.Hash][]string) string {
	var parts []string

	if commitHash == headHash {
		if branchName != "" {
			parts = append(parts, "HEAD -> "+branchName)
		} else {
			parts = append(parts, "HEAD")
		}
	}

	if refs, ok := refDecorations[commitHash]; ok {
		for _, ref := range refs {
			// Skip the current branch since it's already shown with HEAD.
			if branchName != "" && ref == branchName {
				continue
			}
			parts = append(parts, ref)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// buildRefDecorations collects all branch and tag refs and maps their
// target hashes to ref display names.
func buildRefDecorations(r *repo.Repo) (map[object.Hash][]string, error) {
	result := make(map[object.Hash][]string)

	branchRefs, err := r.ListRefs("heads")
	if err != nil {
		return nil, fmt.Errorf("list branch refs: %w", err)
	}
	for name, hash := range branchRefs {
		// name is like "heads/main" -> display as "main"
		displayName := strings.TrimPrefix(name, "heads/")
		result[hash] = append(result[hash], displayName)
	}

	tagRefs, err := r.ListRefs("tags")
	if err != nil {
		return nil, fmt.Errorf("list tag refs: %w", err)
	}
	for name, hash := range tagRefs {
		displayName := "tag: " + strings.TrimPrefix(name, "tags/")
		result[hash] = append(result[hash], displayName)
	}

	return result, nil
}

type logEntitySelector struct {
	Path string
	Key  string
}

func parseLogEntitySelector(raw string) (logEntitySelector, error) {
	selector := strings.TrimSpace(raw)
	if selector == "" {
		return logEntitySelector{}, fmt.Errorf("entity selector cannot be empty")
	}

	idx := strings.Index(selector, "::")
	if idx < 0 {
		return logEntitySelector{Key: selector}, nil
	}

	pathPart := strings.TrimSpace(selector[:idx])
	keyPart := strings.TrimSpace(selector[idx+2:])
	if pathPart == "" || keyPart == "" {
		return logEntitySelector{}, fmt.Errorf("invalid entity selector %q; expected path::entity_key or entity_key", raw)
	}
	if !looksLikePathSelector(pathPart) {
		return logEntitySelector{Key: selector}, nil
	}

	normalizedPath := filepath.ToSlash(filepath.Clean(pathPart))
	if normalizedPath == "." {
		return logEntitySelector{}, fmt.Errorf("invalid entity selector %q; path must identify a file", raw)
	}

	return logEntitySelector{
		Path: normalizedPath,
		Key:  keyPart,
	}, nil
}

func looksLikePathSelector(pathPart string) bool {
	if strings.Contains(pathPart, "/") || strings.Contains(pathPart, "\\") {
		return true
	}
	// Keep declaration keys like "decl:function_declaration::..." as plain keys.
	if strings.Contains(pathPart, ":") && !isWindowsDrivePath(pathPart) {
		return false
	}
	return true
}

func isWindowsDrivePath(pathPart string) bool {
	if len(pathPart) < 2 {
		return false
	}
	if pathPart[1] != ':' {
		return false
	}
	drive := pathPart[0]
	return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
}

// renderGraph produces an ASCII graph prefix for each commit in the log.
// Each commit occupies one lane. Merge commits show lines joining from
// their secondary parents. The output slice has one entry per commit,
// containing the graph prefix string for that line.
func renderGraph(entries []repo.LogEntry) []string {
	if len(entries) == 0 {
		return nil
	}

	// Build a hash->index map for quick parent lookup.
	idxOf := make(map[object.Hash]int, len(entries))
	for i, e := range entries {
		idxOf[e.Hash] = i
	}

	// Lanes: each entry in lanes is the hash of the commit expected in that lane.
	// A lane is "active" if we still expect a commit to appear in it.
	var lanes []object.Hash
	result := make([]string, len(entries))

	for i, entry := range entries {
		h := entry.Hash

		// Find which lane this commit is in (if any).
		myLane := -1
		for l, lh := range lanes {
			if lh == h {
				myLane = l
				break
			}
		}

		// If this commit isn't in any lane yet, add a new lane.
		if myLane == -1 {
			myLane = len(lanes)
			lanes = append(lanes, h)
		}

		// Build the graph line for this commit.
		var buf strings.Builder
		for l := 0; l < len(lanes); l++ {
			if l > 0 {
				buf.WriteByte(' ')
			}
			if l == myLane {
				buf.WriteByte('*')
			} else {
				buf.WriteByte('|')
			}
		}
		result[i] = buf.String()

		// Update lanes based on this commit's parents.
		parents := entry.Commit.Parents

		if len(parents) == 0 {
			// Terminal commit: close this lane.
			lanes = removeLane(lanes, myLane)
		} else {
			// First parent takes over this lane.
			lanes[myLane] = parents[0]

			// Secondary parents open new lanes (merges).
			for _, p := range parents[1:] {
				// Only add a lane if this parent isn't already in a lane.
				found := false
				for _, lh := range lanes {
					if lh == p {
						found = true
						break
					}
				}
				if !found {
					// Check if this parent is actually in our entries list.
					if _, inLog := idxOf[p]; inLog {
						lanes = append(lanes, p)
					}
				}
			}
		}

		// Deduplicate lanes: if the same hash appears multiple times,
		// keep only the first occurrence.
		lanes = deduplicateLanes(lanes)
	}

	return result
}

// removeLane removes the lane at the given index, shifting later lanes left.
func removeLane(lanes []object.Hash, idx int) []object.Hash {
	return append(lanes[:idx], lanes[idx+1:]...)
}

// deduplicateLanes removes duplicate hashes from lanes, keeping first occurrence.
func deduplicateLanes(lanes []object.Hash) []object.Hash {
	seen := make(map[object.Hash]struct{}, len(lanes))
	result := make([]object.Hash, 0, len(lanes))
	for _, h := range lanes {
		if _, dup := seen[h]; !dup {
			seen[h] = struct{}{}
			result = append(result, h)
		}
	}
	return result
}

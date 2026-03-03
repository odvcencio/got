package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/graft/pkg/diff"
	"github.com/odvcencio/graft/pkg/diff3"
	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

const lineDiffContextLines = 3

func newDiffCmd() *cobra.Command {
	var staged bool
	var entity bool
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show changes between working tree, staging, and HEAD",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			if jsonFlag {
				if entity {
					return fmt.Errorf("--json and --entity cannot be combined")
				}
				if staged {
					return diffStagedJSON(cmd, r)
				}
				return diffUnstagedJSON(cmd, r)
			}
			if staged {
				return diffStaged(cmd, r, entity)
			}
			return diffUnstaged(cmd, r, entity)
		},
	}

	cmd.Flags().BoolVar(&staged, "staged", false, "show staged changes (staging vs HEAD)")
	cmd.Flags().BoolVar(&entity, "entity", false, "show entity-level structural diff")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}

// diffUnstaged compares the working tree against the staging area.
func diffUnstaged(cmd *cobra.Command, r *repo.Repo, entityMode bool) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return err
	}
	statusEntries, err := r.Status()
	if err != nil {
		return err
	}
	workRenamedOldToNew := make(map[string]string)
	for _, e := range statusEntries {
		if e.WorkStatus == repo.StatusRenamed && e.RenamedFrom != "" {
			workRenamedOldToNew[e.RenamedFrom] = e.Path
		}
	}

	// Sort paths for deterministic output.
	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := cmd.OutOrStdout()

	for _, p := range paths {
		se := stg.Entries[p]

		absPath := filepath.Join(r.RootDir, filepath.FromSlash(p))
		workData, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				if newPath, renamed := workRenamedOldToNew[p]; renamed {
					printRename(out, p, newPath)
					continue
				}
				// File deleted from working tree -- show full deletion.
				stagedBlob, blobErr := r.Store.ReadBlob(se.BlobHash)
				if blobErr != nil {
					return fmt.Errorf("diff: read staged blob %s: %w", p, blobErr)
				}
				if err := printDiff(out, p, stagedBlob.Data, nil, entityMode); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("diff: read %s: %w", p, err)
		}

		// Compare working copy hash against staged blob hash.
		workHash := object.HashObject(object.TypeBlob, workData)
		if workHash == se.BlobHash {
			continue // unchanged
		}

		stagedBlob, err := r.Store.ReadBlob(se.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read staged blob %s: %w", p, err)
		}

		if err := printDiff(out, p, stagedBlob.Data, workData, entityMode); err != nil {
			return err
		}
	}

	return nil
}

// diffStaged compares the staging area against the HEAD commit tree.
func diffStaged(cmd *cobra.Command, r *repo.Repo, entityMode bool) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return err
	}
	statusEntries, err := r.Status()
	if err != nil {
		return err
	}
	indexRenamedNewToOld := make(map[string]string)
	indexRenamedOld := make(map[string]struct{})
	for _, e := range statusEntries {
		if e.IndexStatus == repo.StatusRenamed && e.RenamedFrom != "" {
			indexRenamedNewToOld[e.Path] = e.RenamedFrom
			indexRenamedOld[e.RenamedFrom] = struct{}{}
		}
	}

	// Build HEAD tree map: path -> TreeFileEntry.
	headMap := make(map[string]repo.TreeFileEntry)
	headHash, err := r.ResolveRef("HEAD")
	if err == nil {
		commit, err := r.Store.ReadCommit(headHash)
		if err == nil {
			entries, err := r.FlattenTree(commit.TreeHash)
			if err == nil {
				for _, e := range entries {
					headMap[e.Path] = e
				}
			}
		}
	}

	// Sort paths for deterministic output.
	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := cmd.OutOrStdout()

	for _, p := range paths {
		se := stg.Entries[p]
		if oldPath, renamed := indexRenamedNewToOld[p]; renamed {
			printRename(out, oldPath, p)
			continue
		}

		headEntry, inHead := headMap[p]
		if inHead && headEntry.BlobHash == se.BlobHash {
			continue // unchanged
		}

		var before []byte
		if inHead {
			blob, err := r.Store.ReadBlob(headEntry.BlobHash)
			if err != nil {
				return fmt.Errorf("diff: read HEAD blob %s: %w", p, err)
			}
			before = blob.Data
		}

		stagedBlob, err := r.Store.ReadBlob(se.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read staged blob %s: %w", p, err)
		}

		if err := printDiff(out, p, before, stagedBlob.Data, entityMode); err != nil {
			return err
		}
	}

	// Check for files deleted from staging that exist in HEAD.
	deletedPaths := make([]string, 0)
	for p := range headMap {
		if _, inStaging := stg.Entries[p]; !inStaging {
			deletedPaths = append(deletedPaths, p)
		}
	}
	sort.Strings(deletedPaths)

	for _, p := range deletedPaths {
		if _, renamed := indexRenamedOld[p]; renamed {
			continue
		}
		headEntry := headMap[p]
		blob, err := r.Store.ReadBlob(headEntry.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read HEAD blob %s: %w", p, err)
		}
		if err := printDiff(out, p, blob.Data, nil, entityMode); err != nil {
			return err
		}
	}

	return nil
}

// printDiff prints a diff for a single file. before or after may be nil for
// additions and deletions respectively.
func printDiff(out io.Writer, path string, before, after []byte, entityMode bool) error {
	if entityMode {
		return printEntityDiff(out, path, before, after)
	}
	return printLineDiff(out, path, before, after)
}

// printEntityDiff uses the structural entity diff to display changes.
func printEntityDiff(out io.Writer, path string, before, after []byte) error {
	if before == nil {
		before = []byte{}
	}
	if after == nil {
		after = []byte{}
	}

	fd, err := diff.DiffFiles(path, before, after)
	if err != nil {
		// Entity extraction not supported for this file type; fall back to line diff.
		return printLineDiff(out, path, before, after)
	}

	s := diff.FormatEntityDiff(fd)
	if s != "" {
		fmt.Fprint(out, s)
	}
	return nil
}

// printLineDiff prints a unified-style line diff for a single file.
func printLineDiff(out io.Writer, path string, before, after []byte) error {
	if before == nil {
		before = []byte{}
	}
	if after == nil {
		after = []byte{}
	}

	if bytes.Equal(before, after) {
		return nil
	}

	fmt.Fprintf(out, "diff --graft a/%s b/%s\n", path, path)
	fmt.Fprintf(out, "--- a/%s\n", path)
	fmt.Fprintf(out, "+++ b/%s\n", path)

	lines := diff3.LineDiff(before, after)
	for _, h := range buildLineDiffHunks(lines, lineDiffContextLines) {
		oldStart, oldCount, newStart, newCount := h.lineRange(lines)
		fmt.Fprintf(out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)

		for _, dl := range lines[h.start:h.end] {
			switch dl.Type {
			case diff3.Equal:
				fmt.Fprintf(out, " %s\n", dl.Content)
			case diff3.Insert:
				fmt.Fprintf(out, "+%s\n", dl.Content)
			case diff3.Delete:
				fmt.Fprintf(out, "-%s\n", dl.Content)
			}
		}
	}

	return nil
}

type lineDiffHunk struct {
	start int
	end   int
}

func buildLineDiffHunks(lines []diff3.DiffLine, contextLines int) []lineDiffHunk {
	if contextLines < 0 {
		contextLines = 0
	}

	var hunks []lineDiffHunk
	for i, dl := range lines {
		if dl.Type == diff3.Equal {
			continue
		}

		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines + 1
		if end > len(lines) {
			end = len(lines)
		}

		if len(hunks) == 0 || start > hunks[len(hunks)-1].end {
			hunks = append(hunks, lineDiffHunk{start: start, end: end})
			continue
		}
		if end > hunks[len(hunks)-1].end {
			hunks[len(hunks)-1].end = end
		}
	}

	return hunks
}

func (h lineDiffHunk) lineRange(lines []diff3.DiffLine) (oldStart, oldCount, newStart, newCount int) {
	oldLine, newLine := 1, 1
	for i := 0; i < h.start; i++ {
		switch lines[i].Type {
		case diff3.Equal:
			oldLine++
			newLine++
		case diff3.Delete:
			oldLine++
		case diff3.Insert:
			newLine++
		}
	}

	oldStart, newStart = oldLine, newLine

	for i := h.start; i < h.end; i++ {
		switch lines[i].Type {
		case diff3.Equal:
			oldCount++
			newCount++
			oldLine++
			newLine++
		case diff3.Delete:
			oldCount++
			oldLine++
		case diff3.Insert:
			newCount++
			newLine++
		}
	}

	if oldCount == 0 {
		oldStart--
	}
	if newCount == 0 {
		newStart--
	}

	return oldStart, oldCount, newStart, newCount
}

func printRename(out io.Writer, fromPath, toPath string) {
	fmt.Fprintf(out, "diff --graft a/%s b/%s\n", fromPath, toPath)
	fmt.Fprintf(out, "rename from %s\n", fromPath)
	fmt.Fprintf(out, "rename to %s\n", toPath)
}

// diffUnstagedJSON collects unstaged diff data and writes JSON output.
func diffUnstagedJSON(cmd *cobra.Command, r *repo.Repo) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return err
	}
	statusEntries, err := r.Status()
	if err != nil {
		return err
	}
	workRenamedOldToNew := make(map[string]string)
	for _, e := range statusEntries {
		if e.WorkStatus == repo.StatusRenamed && e.RenamedFrom != "" {
			workRenamedOldToNew[e.RenamedFrom] = e.Path
		}
	}

	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]JSONDiffFile, 0)

	for _, p := range paths {
		se := stg.Entries[p]

		absPath := filepath.Join(r.RootDir, filepath.FromSlash(p))
		workData, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				if newPath, renamed := workRenamedOldToNew[p]; renamed {
					files = append(files, JSONDiffFile{
						Path:        newPath,
						Status:      "renamed",
						RenamedFrom: p,
						RenamedTo:   newPath,
					})
					continue
				}
				stagedBlob, blobErr := r.Store.ReadBlob(se.BlobHash)
				if blobErr != nil {
					return fmt.Errorf("diff: read staged blob %s: %w", p, blobErr)
				}
				files = append(files, buildJSONDiffFile(p, stagedBlob.Data, nil))
				continue
			}
			return fmt.Errorf("diff: read %s: %w", p, err)
		}

		workHash := object.HashObject(object.TypeBlob, workData)
		if workHash == se.BlobHash {
			continue
		}

		stagedBlob, err := r.Store.ReadBlob(se.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read staged blob %s: %w", p, err)
		}

		files = append(files, buildJSONDiffFile(p, stagedBlob.Data, workData))
	}

	return writeJSON(cmd.OutOrStdout(), JSONDiffOutput{Files: files})
}

// diffStagedJSON collects staged diff data and writes JSON output.
func diffStagedJSON(cmd *cobra.Command, r *repo.Repo) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return err
	}
	statusEntries, err := r.Status()
	if err != nil {
		return err
	}
	indexRenamedNewToOld := make(map[string]string)
	indexRenamedOld := make(map[string]struct{})
	for _, e := range statusEntries {
		if e.IndexStatus == repo.StatusRenamed && e.RenamedFrom != "" {
			indexRenamedNewToOld[e.Path] = e.RenamedFrom
			indexRenamedOld[e.RenamedFrom] = struct{}{}
		}
	}

	headMap := make(map[string]repo.TreeFileEntry)
	headHash, err := r.ResolveRef("HEAD")
	if err == nil {
		commit, err := r.Store.ReadCommit(headHash)
		if err == nil {
			entries, err := r.FlattenTree(commit.TreeHash)
			if err == nil {
				for _, e := range entries {
					headMap[e.Path] = e
				}
			}
		}
	}

	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]JSONDiffFile, 0)

	for _, p := range paths {
		se := stg.Entries[p]
		if oldPath, renamed := indexRenamedNewToOld[p]; renamed {
			files = append(files, JSONDiffFile{
				Path:        p,
				Status:      "renamed",
				RenamedFrom: oldPath,
				RenamedTo:   p,
			})
			continue
		}

		headEntry, inHead := headMap[p]
		if inHead && headEntry.BlobHash == se.BlobHash {
			continue
		}

		var before []byte
		if inHead {
			blob, err := r.Store.ReadBlob(headEntry.BlobHash)
			if err != nil {
				return fmt.Errorf("diff: read HEAD blob %s: %w", p, err)
			}
			before = blob.Data
		}

		stagedBlob, err := r.Store.ReadBlob(se.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read staged blob %s: %w", p, err)
		}

		files = append(files, buildJSONDiffFile(p, before, stagedBlob.Data))
	}

	// Check for files deleted from staging that exist in HEAD.
	deletedPaths := make([]string, 0)
	for p := range headMap {
		if _, inStaging := stg.Entries[p]; !inStaging {
			deletedPaths = append(deletedPaths, p)
		}
	}
	sort.Strings(deletedPaths)

	for _, p := range deletedPaths {
		if _, renamed := indexRenamedOld[p]; renamed {
			continue
		}
		headEntry := headMap[p]
		blob, err := r.Store.ReadBlob(headEntry.BlobHash)
		if err != nil {
			return fmt.Errorf("diff: read HEAD blob %s: %w", p, err)
		}
		files = append(files, buildJSONDiffFile(p, blob.Data, nil))
	}

	return writeJSON(cmd.OutOrStdout(), JSONDiffOutput{Files: files})
}

// buildJSONDiffFile constructs a JSONDiffFile from before/after content.
func buildJSONDiffFile(path string, before, after []byte) JSONDiffFile {
	if before == nil {
		before = []byte{}
	}
	if after == nil {
		after = []byte{}
	}

	status := "modified"
	if len(before) == 0 {
		status = "added"
	} else if len(after) == 0 {
		status = "deleted"
	}

	if bytes.Equal(before, after) {
		return JSONDiffFile{Path: path, Status: status}
	}

	lines := diff3.LineDiff(before, after)
	hunks := buildLineDiffHunks(lines, lineDiffContextLines)

	var jsonHunks []JSONDiffHunk
	for _, h := range hunks {
		oldStart, oldCount, newStart, newCount := h.lineRange(lines)
		var jsonLines []JSONDiffLine
		for _, dl := range lines[h.start:h.end] {
			var lineType string
			switch dl.Type {
			case diff3.Equal:
				lineType = "context"
			case diff3.Insert:
				lineType = "add"
			case diff3.Delete:
				lineType = "delete"
			}
			jsonLines = append(jsonLines, JSONDiffLine{
				Type:    lineType,
				Content: dl.Content,
			})
		}
		jsonHunks = append(jsonHunks, JSONDiffHunk{
			OldStart: oldStart,
			OldCount: oldCount,
			NewStart: newStart,
			NewCount: newCount,
			Lines:    jsonLines,
		})
	}

	return JSONDiffFile{
		Path:  path,
		Status: status,
		Hunks: jsonHunks,
	}
}

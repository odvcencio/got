package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/merge"
)

// ConflictEntry describes a single entity-level conflict within a file.
type ConflictEntry struct {
	Path         string
	EntityKey    string // empty for non-structural conflicts
	EntityName   string // human-readable name (e.g. "func ProcessOrder")
	EntityKind   string // entity kind string (e.g. "declaration")
	ConflictType string // merge.ConflictTypeBothModified, merge.ConflictTypeDeleteVsModify, or "text"
}

// ListConflicts returns all entity-level conflicts from the current staging area.
// It reads the staging index for entries with Conflict=true, then scans the
// working directory file for annotated conflict markers to extract entity details.
//
// For files with structural (entity-annotated) conflict markers, each entity
// conflict gets its own ConflictEntry. For files with plain conflict markers
// (text fallback or binary), a single ConflictEntry with EntityName="" is returned.
func (r *Repo) ListConflicts() ([]ConflictEntry, error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, err
	}

	// Collect conflicted paths in sorted order for deterministic output.
	var paths []string
	for path, entry := range stg.Entries {
		if entry.Conflict {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	var entries []ConflictEntry
	for _, path := range paths {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		fileEntries, err := parseConflictMarkers(path, absPath)
		if err != nil {
			// If the file can't be read (deleted?), report the path with no details.
			entries = append(entries, ConflictEntry{
				Path:         path,
				ConflictType: "text",
			})
			continue
		}
		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

// parseConflictMarkers scans a file for conflict markers and extracts entity
// information from annotations. Returns one ConflictEntry per conflict block found.
//
// Annotated markers look like:
//
//	<<<<<<< ours (func ProcessOrder)
//
// Plain markers (from text fallback) look like:
//
//	<<<<<<< ours
func parseConflictMarkers(path, absPath string) ([]ConflictEntry, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []ConflictEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "<<<<<<< ours") {
			continue
		}

		entry := ConflictEntry{Path: path}

		// Try to extract annotation: "<<<<<<< ours (func ProcessOrder)"
		annotation := extractAnnotation(line)
		if annotation != "" {
			entry.EntityName = annotation
			entry.ConflictType = conflictTypeFromAnnotation(annotation)
			entry.EntityKind = "declaration"
		} else {
			entry.ConflictType = "text"
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// If no markers found but the file is in conflict state, add a single entry.
	if len(entries) == 0 {
		entries = append(entries, ConflictEntry{
			Path:         path,
			ConflictType: "text",
		})
	}

	return entries, nil
}

// extractAnnotation parses the entity name from a conflict marker line.
// Given "<<<<<<< ours (func ProcessOrder)" it returns "func ProcessOrder".
// Returns "" if no annotation is present.
func extractAnnotation(line string) string {
	// Look for " (" after "<<<<<<< ours"
	idx := strings.Index(line, " (")
	if idx < 0 {
		return ""
	}
	rest := line[idx+2:]
	end := strings.LastIndex(rest, ")")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// conflictTypeFromAnnotation infers the conflict type from the entity annotation.
// Currently defaults to "both_modified" since delete-vs-modify conflicts also
// get markers but the type distinction requires merge-time context that isn't
// embedded in the marker. The MergeReport's EntityConflicts has the authoritative
// type; this is a best-effort parse for ListConflicts().
func conflictTypeFromAnnotation(annotation string) string {
	// The annotation itself doesn't encode the conflict type.
	// Default to both_modified -- the most common structural conflict.
	_ = annotation
	return merge.ConflictTypeBothModified
}

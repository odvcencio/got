package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ReadConflictBodies reads the base, ours, and theirs blob content for a
// conflicted file from the staging area's recorded blob hashes. This provides
// the raw three-way content that an AI resolver needs.
func (r *Repo) ReadConflictBodies(path string) (base, ours, theirs []byte, err error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read staging: %w", err)
	}

	entry, ok := stg.Entries[path]
	if !ok {
		return nil, nil, nil, fmt.Errorf("file %q not in staging", path)
	}
	if !entry.Conflict {
		return nil, nil, nil, fmt.Errorf("file %q is not in conflict", path)
	}

	if entry.BaseBlobHash != "" {
		blob, err := r.Store.ReadBlob(entry.BaseBlobHash)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read base blob: %w", err)
		}
		base = blob.Data
	}

	if entry.OursBlobHash != "" {
		blob, err := r.Store.ReadBlob(entry.OursBlobHash)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read ours blob: %w", err)
		}
		ours = blob.Data
	}

	if entry.TheirsBlobHash != "" {
		blob, err := r.Store.ReadBlob(entry.TheirsBlobHash)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read theirs blob: %w", err)
		}
		theirs = blob.Data
	}

	return base, ours, theirs, nil
}

// ApplyAIResolution replaces a single entity's conflict markers in a file
// with the AI-resolved content. The entityName is matched against the
// annotation in "<<<<<<< ours (entityName)".
//
// If all conflicts in the file are resolved after this replacement, the
// staging entry's Conflict flag is cleared.
func (r *Repo) ApplyAIResolution(path, entityName string, resolvedBody []byte) error {
	absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	result, replaced := replaceConflictBlock(data, entityName, resolvedBody)
	if !replaced {
		return fmt.Errorf("conflict markers for %q not found in %s", entityName, path)
	}

	// Atomic write: temp file + rename to prevent partial writes.
	tmp, err := os.CreateTemp(filepath.Dir(absPath), ".graft-resolve-*")
	if err != nil {
		return fmt.Errorf("write resolved file: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(result); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write resolved file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write resolved file: close: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write resolved file: rename: %w", err)
	}

	// Check if any conflict markers remain in the file.
	if !hasConflictMarkers(result) {
		// All conflicts resolved — update staging to mark file as non-conflict.
		if err := r.markConflictResolved(path); err != nil {
			return fmt.Errorf("update staging: %w", err)
		}
	}

	return nil
}

// replaceConflictBlock finds and replaces a conflict block annotated with the
// given entity name. Returns the modified content and whether a replacement
// was made.
func replaceConflictBlock(data []byte, entityName string, resolvedBody []byte) ([]byte, bool) {
	// Look for: <<<<<<< ours (entityName)
	marker := fmt.Sprintf("<<<<<<< ours (%s)", entityName)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var result bytes.Buffer
	found := false
	inConflict := false

	for scanner.Scan() {
		line := scanner.Text()

		if !inConflict && strings.HasPrefix(line, marker) {
			// Start of the target conflict block.
			inConflict = true
			found = true
			// Write the resolved body instead.
			result.Write(resolvedBody)
			if len(resolvedBody) > 0 && resolvedBody[len(resolvedBody)-1] != '\n' {
				result.WriteByte('\n')
			}
			continue
		}

		if inConflict {
			// Skip lines until we find the end marker.
			if strings.HasPrefix(line, ">>>>>>> theirs") {
				inConflict = false
				continue
			}
			// Skip ours body, separator, and theirs body.
			continue
		}

		result.WriteString(line)
		result.WriteByte('\n')
	}

	return result.Bytes(), found
}

// hasConflictMarkers returns true if the content contains any conflict markers.
func hasConflictMarkers(data []byte) bool {
	return bytes.Contains(data, []byte("<<<<<<< ours"))
}

// markConflictResolved updates the staging entry for a file to clear the
// Conflict flag and update the blob hash to the current file content.
func (r *Repo) markConflictResolved(path string) error {
	absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	// Re-stage the file to update blob hash and clear conflict.
	stg, err := r.ReadStaging()
	if err != nil {
		return err
	}

	entry, ok := stg.Entries[path]
	if !ok {
		return nil
	}

	blob, err := r.Store.WriteBlob(&object.Blob{Data: data})
	if err != nil {
		return err
	}

	entry.BlobHash = blob
	entry.Conflict = false
	entry.BaseBlobHash = ""
	entry.OursBlobHash = ""
	entry.TheirsBlobHash = ""

	info, err := os.Stat(absPath)
	if err == nil {
		entry.ModTime = info.ModTime().UnixNano()
		entry.Size = info.Size()
	}

	stg.Entries[path] = entry
	return r.WriteStaging(stg)
}

package main

import (
	"encoding/json"
	"io"
)

// writeJSON encodes v as indented JSON and writes it to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- Status ---

// JSONStatusOutput is the top-level JSON output for "graft status --json".
type JSONStatusOutput struct {
	Branch    string             `json:"branch"`
	NoCommits bool               `json:"noCommits"`
	Conflicts []JSONStatusEntry  `json:"conflicts,omitempty"`
	Staged    []JSONStatusEntry  `json:"staged,omitempty"`
	Unstaged  []JSONStatusEntry  `json:"unstaged,omitempty"`
	Untracked []string           `json:"untracked,omitempty"`
}

// JSONStatusEntry represents a single file in a status category.
type JSONStatusEntry struct {
	Path        string `json:"path"`
	Status      string `json:"status"`                // "new", "modified", "deleted", "renamed", "conflict", "dirty"
	RenamedFrom string `json:"renamedFrom,omitempty"`
}

// --- Diff ---

// JSONDiffEntityChange represents a single entity-level change in a diff.
type JSONDiffEntityChange struct {
	Path       string `json:"path"`
	EntityKey  string `json:"entityKey"`
	ChangeType string `json:"changeType"`
}

// JSONDiffOutput is the top-level JSON output for "graft diff --json".
type JSONDiffOutput struct {
	Files         []JSONDiffFile         `json:"files"`
	EntityChanges []JSONDiffEntityChange `json:"entityChanges,omitempty"`
}

// JSONDiffFile represents a single file's diff.
type JSONDiffFile struct {
	Path        string          `json:"path"`
	Status      string          `json:"status"` // "modified", "added", "deleted", "renamed"
	RenamedFrom string          `json:"renamedFrom,omitempty"`
	RenamedTo   string          `json:"renamedTo,omitempty"`
	Hunks       []JSONDiffHunk  `json:"hunks,omitempty"`
}

// JSONDiffHunk represents a single hunk in a unified diff.
type JSONDiffHunk struct {
	OldStart int              `json:"oldStart"`
	OldCount int              `json:"oldCount"`
	NewStart int              `json:"newStart"`
	NewCount int              `json:"newCount"`
	Lines    []JSONDiffLine   `json:"lines"`
}

// JSONDiffLine represents a single line in a diff hunk.
type JSONDiffLine struct {
	Type    string `json:"type"` // "context", "add", "delete"
	Content string `json:"content"`
}

// --- Log ---

// JSONLogOutput is the top-level JSON output for "graft log --json".
type JSONLogOutput struct {
	Commits []JSONLogEntry `json:"commits"`
}

// JSONLogEntry represents a single commit in the log.
type JSONLogEntry struct {
	Hash       string   `json:"hash"`
	ShortHash  string   `json:"shortHash"`
	Author     string   `json:"author"`
	Date       string   `json:"date"`
	Timestamp  int64    `json:"timestamp"`
	Message    string   `json:"message"`
	Parents    []string `json:"parents,omitempty"`
	Decoration string   `json:"decoration,omitempty"`
}

// --- Merge ---

// JSONMergeOutput is the top-level JSON output for "graft merge --json".
type JSONMergeOutput struct {
	Action         string              `json:"action"` // "merge", "abort", "preview"
	Source         string              `json:"source,omitempty"`
	Target         string              `json:"target,omitempty"`
	IsFastForward  bool                `json:"isFastForward"`
	HasConflicts   bool                `json:"hasConflicts"`
	TotalConflicts int                 `json:"totalConflicts"`
	MergeCommit    string              `json:"mergeCommit,omitempty"`
	Files          []JSONMergeFile     `json:"files,omitempty"`
	Message        string              `json:"message,omitempty"`
}

// JSONMergeFile represents the merge status of a single file.
type JSONMergeFile struct {
	Path            string                `json:"path"`
	Status          string                `json:"status"` // "clean", "conflict", "added", "deleted"
	EntityCount     int                   `json:"entityCount,omitempty"`
	ConflictCount   int                   `json:"conflictCount,omitempty"`
	EntityConflicts []JSONEntityConflict  `json:"entityConflicts,omitempty"`
}

// JSONEntityConflict represents a single entity-level conflict within a file.
type JSONEntityConflict struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// --- Show ---

// JSONShowOutput is the top-level JSON output for "graft show --json".
type JSONShowOutput struct {
	Hash    string           `json:"hash"`
	Author  string           `json:"author"`
	Date    string           `json:"date"`
	Timestamp int64          `json:"timestamp"`
	Message string           `json:"message"`
	Parents []string         `json:"parents,omitempty"`
	Changes []JSONShowChange `json:"changes,omitempty"`
}

// JSONShowChange represents a file changed in a commit.
type JSONShowChange struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "A" (added), "D" (deleted), "M" (modified)
}

// --- Blame ---

// JSONBlameOutput is the JSON output for "graft blame --entity --json".
type JSONBlameOutput struct {
	Path       string `json:"path"`
	EntityKey  string `json:"entityKey"`
	Author     string `json:"author"`
	CommitHash string `json:"commitHash"`
	Message    string `json:"message"`
}

// JSONBatchBlameOutput is the JSON output for "graft blame <path> --json".
type JSONBatchBlameOutput struct {
	Path     string            `json:"path"`
	Entities []JSONBlameOutput `json:"entities"`
}

// --- Conflicts ---

// JSONConflictsOutput is the top-level JSON output for "graft conflicts --json".
type JSONConflictsOutput struct {
	Files []JSONConflictFile `json:"files"`
}

// JSONConflictFile represents a file with conflicts.
type JSONConflictFile struct {
	Path     string                `json:"path"`
	Entities []JSONConflictEntity  `json:"entities"`
}

// JSONConflictEntity represents a single entity conflict within a file.
type JSONConflictEntity struct {
	EntityName   string `json:"entityName,omitempty"`
	EntityKey    string `json:"entityKey,omitempty"`
	EntityKind   string `json:"entityKind,omitempty"`
	ConflictType string `json:"conflictType"`
}

// --- Verify ---

// JSONVerifyOutput is the top-level JSON output for "graft verify --json".
type JSONVerifyOutput struct {
	Results      []JSONVerifyResult `json:"results,omitempty"`
	LooseObjects int                `json:"looseObjects,omitempty"`
	PackFiles    int                `json:"packFiles,omitempty"`
	PackObjects  int                `json:"packObjects,omitempty"`
}

// JSONVerifyResult represents the signature verification result for a single commit.
type JSONVerifyResult struct {
	CommitHash string `json:"commitHash"`
	Valid      bool   `json:"valid"`
	Unsigned   bool   `json:"unsigned,omitempty"`
	SignerKey  string `json:"signerKey,omitempty"`
	Algorithm  string `json:"algorithm,omitempty"`
	Error      string `json:"error,omitempty"`
}

// --- Entity Search ---

// JSONEntitySearchOutput is the top-level JSON output for "graft grep --entity --json".
type JSONEntitySearchOutput struct {
	Results []JSONEntitySearchResult `json:"results"`
}

// JSONEntitySearchResult represents a single entity match.
type JSONEntitySearchResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	DeclKind string `json:"declKind"`
	Key      string `json:"key"`
}

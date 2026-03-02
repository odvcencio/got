// Package object implements the content-addressed object store for graft,
// supporting SHA-256 hashed blobs, entities, entity lists, trees, commits,
// and tags with zlib compression and pack file delta encoding.
package object

import "fmt"

// Hash is a 64-character hex-encoded SHA-256 digest.
type Hash string

// ValidateHash checks that s is a well-formed 64-character lowercase hex hash.
// Returns an error if the hash is invalid.
func ValidateHash(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("invalid hash length %d (expected 64): %q", len(s), truncateForError(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("invalid hash character at position %d: %q", i, truncateForError(s))
		}
	}
	return nil
}

// truncateForError truncates s to 80 characters for error messages.
func truncateForError(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// ObjectType identifies the kind of object stored.
type ObjectType string

const (
	TypeBlob       ObjectType = "blob"
	TypeTag        ObjectType = "tag"
	TypeEntity     ObjectType = "entity"
	TypeEntityList ObjectType = "entitylist"
	TypeTree       ObjectType = "tree"
	TypeCommit     ObjectType = "commit"
)

const (
	// Tree mode constants compatible with Git's canonical mode strings.
	TreeModeDir        = "40000"
	TreeModeFile       = "100644"
	TreeModeExecutable = "100755"
	TreeModeModule     = "160000"
)

// Blob holds raw file data.
type Blob struct {
	Data []byte
}

// TagObj preserves annotated tag payload while tracking the referenced object.
// Data stores the canonical tag bytes, where the "object" header points at the
// graft hash (not git hash) so graph traversal can stay in graft object space.
type TagObj struct {
	TargetHash Hash
	Data       []byte
}

// EntityObj represents a single code entity (function, type, etc.).
type EntityObj struct {
	Kind     string // e.g. "function", "type", "method"
	Name     string
	DeclKind string // language-specific declaration kind
	Receiver string // method receiver, empty for non-methods
	Body     []byte
	BodyHash Hash
}

// EntityListObj is an ordered list of entity references for a file.
type EntityListObj struct {
	Language   string
	Path       string
	EntityRefs []Hash // ordered refs to EntityObj hashes
}

// TreeEntry is one entry in a tree object.
type TreeEntry struct {
	Name           string
	IsDir          bool
	Mode           string
	BlobHash       Hash
	EntityListHash Hash
	SubtreeHash    Hash
}

// TreeObj holds a sorted list of tree entries.
type TreeObj struct {
	Entries []TreeEntry // sorted by Name
}

// CommitObj represents a commit pointing to a tree with metadata.
type CommitObj struct {
	TreeHash           Hash
	Parents            []Hash
	Author             string
	Timestamp          int64
	AuthorTimezone     string
	Committer          string
	CommitterTimestamp int64
	CommitterTimezone  string
	Signature          string
	Message            string
}

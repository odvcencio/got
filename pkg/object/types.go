package object

// Hash is a 64-character hex-encoded SHA-256 digest.
type Hash string

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
)

// Blob holds raw file data.
type Blob struct {
	Data []byte
}

// TagObj preserves annotated tag payload while tracking the referenced object.
// Data stores the canonical tag bytes, where the "object" header points at the
// got hash (not git hash) so graph traversal can stay in got object space.
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

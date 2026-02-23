package object

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Store is a content-addressed object store with a 2-character fan-out
// directory layout: objects/ab/cdef0123...
type Store struct {
	root string
}

// NewStore creates a Store rooted at the given directory. The objects/
// subdirectory is created lazily on first write.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// objectPath returns the filesystem path for a given hash.
func (s *Store) objectPath(h Hash) string {
	return filepath.Join(s.root, "objects", string(h[:2]), string(h[2:]))
}

// Has reports whether the store contains an object with the given hash.
func (s *Store) Has(h Hash) bool {
	_, err := os.Stat(s.objectPath(h))
	return err == nil
}

// Write stores an object and returns its content hash. The on-disk format
// is "type len\0content". Writes are atomic: data is written to a temp
// file and then renamed into place.
func (s *Store) Write(objType ObjectType, data []byte) (Hash, error) {
	envelope := fmt.Sprintf("%s %d\x00", objType, len(data))
	raw := append([]byte(envelope), data...)

	h := HashObject(objType, data)

	// Fast path: already exists.
	if s.Has(h) {
		return h, nil
	}

	dir := filepath.Join(s.root, "objects", string(h[:2]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("object write mkdir: %w", err)
	}

	// Atomic write via temp + rename.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("object write tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("object write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("object write close: %w", err)
	}

	dest := s.objectPath(h)
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("object write rename: %w", err)
	}

	return h, nil
}

// Read retrieves an object by hash, returning its type and raw content.
func (s *Store) Read(h Hash) (ObjectType, []byte, error) {
	raw, err := os.ReadFile(s.objectPath(h))
	if err != nil {
		return "", nil, fmt.Errorf("object read %s: %w", h, err)
	}

	// Parse envelope: "type len\0content"
	nulIdx := bytes.IndexByte(raw, 0)
	if nulIdx < 0 {
		return "", nil, fmt.Errorf("object read %s: invalid format (no NUL)", h)
	}
	header := string(raw[:nulIdx])
	content := raw[nulIdx+1:]

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("object read %s: invalid header %q", h, header)
	}
	objType := ObjectType(parts[0])
	length, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("object read %s: invalid length %q: %w", h, parts[1], err)
	}
	if len(content) != length {
		return "", nil, fmt.Errorf("object read %s: length mismatch (header=%d, actual=%d)", h, length, len(content))
	}

	return objType, content, nil
}

// ---------------------------------------------------------------------------
// Typed convenience methods
// ---------------------------------------------------------------------------

// WriteBlob serializes and stores a Blob.
func (s *Store) WriteBlob(b *Blob) (Hash, error) {
	return s.Write(TypeBlob, MarshalBlob(b))
}

// ReadBlob reads and deserializes a Blob.
func (s *Store) ReadBlob(h Hash) (*Blob, error) {
	objType, data, err := s.Read(h)
	if err != nil {
		return nil, err
	}
	if objType != TypeBlob {
		return nil, fmt.Errorf("object %s: type mismatch: got %q, want %q", h, objType, TypeBlob)
	}
	return UnmarshalBlob(data)
}

// WriteEntity serializes and stores an EntityObj.
func (s *Store) WriteEntity(e *EntityObj) (Hash, error) {
	return s.Write(TypeEntity, MarshalEntity(e))
}

// ReadEntity reads and deserializes an EntityObj.
func (s *Store) ReadEntity(h Hash) (*EntityObj, error) {
	objType, data, err := s.Read(h)
	if err != nil {
		return nil, err
	}
	if objType != TypeEntity {
		return nil, fmt.Errorf("object %s: type mismatch: got %q, want %q", h, objType, TypeEntity)
	}
	return UnmarshalEntity(data)
}

// WriteEntityList serializes and stores an EntityListObj.
func (s *Store) WriteEntityList(el *EntityListObj) (Hash, error) {
	return s.Write(TypeEntityList, MarshalEntityList(el))
}

// ReadEntityList reads and deserializes an EntityListObj.
func (s *Store) ReadEntityList(h Hash) (*EntityListObj, error) {
	objType, data, err := s.Read(h)
	if err != nil {
		return nil, err
	}
	if objType != TypeEntityList {
		return nil, fmt.Errorf("object %s: type mismatch: got %q, want %q", h, objType, TypeEntityList)
	}
	return UnmarshalEntityList(data)
}

// WriteTree serializes and stores a TreeObj.
func (s *Store) WriteTree(tr *TreeObj) (Hash, error) {
	return s.Write(TypeTree, MarshalTree(tr))
}

// ReadTree reads and deserializes a TreeObj.
func (s *Store) ReadTree(h Hash) (*TreeObj, error) {
	objType, data, err := s.Read(h)
	if err != nil {
		return nil, err
	}
	if objType != TypeTree {
		return nil, fmt.Errorf("object %s: type mismatch: got %q, want %q", h, objType, TypeTree)
	}
	return UnmarshalTree(data)
}

// WriteCommit serializes and stores a CommitObj.
func (s *Store) WriteCommit(c *CommitObj) (Hash, error) {
	return s.Write(TypeCommit, MarshalCommit(c))
}

// ReadCommit reads and deserializes a CommitObj.
func (s *Store) ReadCommit(h Hash) (*CommitObj, error) {
	objType, data, err := s.Read(h)
	if err != nil {
		return nil, err
	}
	if objType != TypeCommit {
		return nil, fmt.Errorf("object %s: type mismatch: got %q, want %q", h, objType, TypeCommit)
	}
	return UnmarshalCommit(data)
}

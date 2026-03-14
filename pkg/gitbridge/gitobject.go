package gitbridge

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
)

// GitObject represents a parsed git object.
type GitObject struct {
	Type string // "blob", "tree", "commit", "tag"
	Data []byte
}

// GitTreeEntry represents an entry in a git tree object.
type GitTreeEntry struct {
	Mode string
	Name string
	Hash []byte // 20 bytes (SHA-1)
}

// ParseGitObject parses a raw git object (type + size + \0 + data).
func ParseGitObject(raw []byte) (*GitObject, error) {
	idx := bytes.IndexByte(raw, 0)
	if idx < 0 {
		return nil, fmt.Errorf("invalid git object: no null separator")
	}
	header := string(raw[:idx])
	parts := bytes.SplitN([]byte(header), []byte(" "), 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid git object header: %q", header)
	}
	objType := string(parts[0])
	size, err := strconv.Atoi(string(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid size in header: %w", err)
	}
	data := raw[idx+1:]
	if len(data) != size {
		return nil, fmt.Errorf("size mismatch: header says %d, got %d", size, len(data))
	}
	return &GitObject{Type: objType, Data: data}, nil
}

// GitObjectHash computes the SHA-1 hash of a git object.
func GitObjectHash(objType string, data []byte) GitHash {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	return GitHash(h.Sum(nil))
}

// GitObjectHashHex returns the hex-encoded SHA-1 hash.
func GitObjectHashHex(objType string, data []byte) string {
	return hex.EncodeToString(GitObjectHash(objType, data))
}

// Helper functions for constructing raw git objects (used in tests and bridge).

func gitBlobBytes(content []byte) []byte {
	header := fmt.Sprintf("blob %d\x00", len(content))
	return append([]byte(header), content...)
}

func gitTreeBytes(entries []GitTreeEntry) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteString(e.Mode)
		buf.WriteByte(' ')
		buf.WriteString(e.Name)
		buf.WriteByte(0)
		buf.Write(e.Hash)
	}
	data := buf.Bytes()
	header := fmt.Sprintf("tree %d\x00", len(data))
	return append([]byte(header), data...)
}

func gitCommitBytes(treeHash, author, message string) []byte {
	content := fmt.Sprintf("tree %s\nauthor %s 1234567890 +0000\ncommitter %s 1234567890 +0000\n\n%s",
		treeHash, author, author, message)
	header := fmt.Sprintf("commit %d\x00", len(content))
	return append([]byte(header), []byte(content)...)
}

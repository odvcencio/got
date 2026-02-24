package object

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Blob
// ---------------------------------------------------------------------------

// MarshalBlob serializes a Blob to raw bytes (identity).
func MarshalBlob(b *Blob) []byte {
	out := make([]byte, len(b.Data))
	copy(out, b.Data)
	return out
}

// UnmarshalBlob deserializes raw bytes into a Blob.
func UnmarshalBlob(data []byte) (*Blob, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return &Blob{Data: out}, nil
}

// ---------------------------------------------------------------------------
// EntityObj
// ---------------------------------------------------------------------------

// MarshalEntity serializes an EntityObj to a deterministic text format:
//
//	kind X
//	name Y
//	declkind Z
//	receiver R
//	bodyhash H
//
//	<body bytes>
func MarshalEntity(e *EntityObj) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "kind %s\n", e.Kind)
	fmt.Fprintf(&buf, "name %s\n", e.Name)
	fmt.Fprintf(&buf, "declkind %s\n", e.DeclKind)
	fmt.Fprintf(&buf, "receiver %s\n", e.Receiver)
	fmt.Fprintf(&buf, "bodyhash %s\n", string(e.BodyHash))
	buf.WriteByte('\n')
	buf.Write(e.Body)
	return buf.Bytes()
}

// UnmarshalEntity parses an EntityObj from its serialized form.
func UnmarshalEntity(data []byte) (*EntityObj, error) {
	// Split at the first blank line (double newline).
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return nil, fmt.Errorf("unmarshal entity: missing header/body separator")
	}
	header := string(data[:idx])
	body := data[idx+2:]

	e := &EntityObj{
		Body: make([]byte, len(body)),
	}
	copy(e.Body, body)

	for _, line := range strings.Split(header, "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			// Allow empty value (e.g. "receiver ")
			key = line
			val = ""
		}
		switch key {
		case "kind":
			e.Kind = val
		case "name":
			e.Name = val
		case "declkind":
			e.DeclKind = val
		case "receiver":
			e.Receiver = val
		case "bodyhash":
			e.BodyHash = Hash(val)
		default:
			return nil, fmt.Errorf("unmarshal entity: unknown header key %q", key)
		}
	}
	return e, nil
}

// ---------------------------------------------------------------------------
// EntityListObj
// ---------------------------------------------------------------------------

// MarshalEntityList serializes an EntityListObj:
//
//	language X
//	path Y
//
//	hash1
//	hash2
//	...
func MarshalEntityList(el *EntityListObj) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "language %s\n", el.Language)
	fmt.Fprintf(&buf, "path %s\n", el.Path)
	buf.WriteByte('\n')
	for _, h := range el.EntityRefs {
		fmt.Fprintf(&buf, "%s\n", string(h))
	}
	return buf.Bytes()
}

// UnmarshalEntityList parses an EntityListObj from its serialized form.
func UnmarshalEntityList(data []byte) (*EntityListObj, error) {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return nil, fmt.Errorf("unmarshal entitylist: missing header/body separator")
	}
	header := string(data[:idx])
	body := string(data[idx+2:])

	el := &EntityListObj{}
	for _, line := range strings.Split(header, "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("unmarshal entitylist: malformed header line %q", line)
		}
		switch key {
		case "language":
			el.Language = val
		case "path":
			el.Path = val
		default:
			return nil, fmt.Errorf("unmarshal entitylist: unknown header key %q", key)
		}
	}

	// Parse hash lines (skip empty trailing lines).
	if strings.TrimSpace(body) != "" {
		for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				el.EntityRefs = append(el.EntityRefs, Hash(line))
			}
		}
	}
	return el, nil
}

// ---------------------------------------------------------------------------
// TreeObj
// ---------------------------------------------------------------------------

// MarshalTree serializes a TreeObj. Entries are sorted by Name for
// deterministic output. Each entry is one line:
//
//	name mode blobhash entitylisthash subtreehash
//
// where mode is a Git-compatible mode string (e.g. 40000, 100644, 100755),
// and empty hashes are represented as "-".
func MarshalTree(tr *TreeObj) []byte {
	// Sort entries by Name for determinism.
	sorted := make([]TreeEntry, len(tr.Entries))
	copy(sorted, tr.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	var buf bytes.Buffer
	for _, e := range sorted {
		mode := treeModeOrDefault(e)
		bh := hashOrDash(e.BlobHash)
		elh := hashOrDash(e.EntityListHash)
		sth := hashOrDash(e.SubtreeHash)
		fmt.Fprintf(&buf, "%s %s %s %s %s\n", e.Name, mode, bh, elh, sth)
	}
	return buf.Bytes()
}

func hashOrDash(h Hash) string {
	if h == "" {
		return "-"
	}
	return string(h)
}

func dashOrHash(s string) Hash {
	if s == "-" {
		return Hash("")
	}
	return Hash(s)
}

// UnmarshalTree parses a TreeObj from its serialized form.
func UnmarshalTree(data []byte) (*TreeObj, error) {
	tr := &TreeObj{}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return tr, nil
	}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, " ", 5)
		if len(parts) != 5 {
			return nil, fmt.Errorf("unmarshal tree: malformed entry %q", line)
		}
		isDir, mode, err := parseTreeMode(parts[1])
		if err != nil {
			return nil, fmt.Errorf("unmarshal tree: %w", err)
		}
		entry := TreeEntry{
			Name:           parts[0],
			IsDir:          isDir,
			Mode:           mode,
			BlobHash:       dashOrHash(parts[2]),
			EntityListHash: dashOrHash(parts[3]),
			SubtreeHash:    dashOrHash(parts[4]),
		}
		tr.Entries = append(tr.Entries, entry)
	}
	return tr, nil
}

func treeModeOrDefault(e TreeEntry) string {
	if e.IsDir {
		return TreeModeDir
	}
	if strings.TrimSpace(e.Mode) == "" {
		return TreeModeFile
	}
	return e.Mode
}

func parseTreeMode(mode string) (bool, string, error) {
	// Backward compatibility for older serialized trees.
	switch mode {
	case "dir":
		return true, TreeModeDir, nil
	case "file":
		return false, TreeModeFile, nil
	}

	switch mode {
	case TreeModeDir:
		return true, TreeModeDir, nil
	case TreeModeFile:
		return false, TreeModeFile, nil
	case TreeModeExecutable:
		return false, TreeModeExecutable, nil
	default:
		return false, "", fmt.Errorf("unknown mode %q", mode)
	}
}

// ---------------------------------------------------------------------------
// CommitObj
// ---------------------------------------------------------------------------

// MarshalCommit serializes a CommitObj:
//
//	tree H
//	parent H     (zero or more)
//	author A
//	timestamp T
//	signature S  (optional)
//
//	message
func MarshalCommit(c *CommitObj) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "tree %s\n", string(c.TreeHash))
	for _, p := range c.Parents {
		fmt.Fprintf(&buf, "parent %s\n", string(p))
	}
	fmt.Fprintf(&buf, "author %s\n", c.Author)
	fmt.Fprintf(&buf, "timestamp %d\n", c.Timestamp)
	if strings.TrimSpace(c.Signature) != "" {
		fmt.Fprintf(&buf, "signature %s\n", c.Signature)
	}
	buf.WriteByte('\n')
	buf.WriteString(c.Message)
	return buf.Bytes()
}

// UnmarshalCommit parses a CommitObj from its serialized form.
func UnmarshalCommit(data []byte) (*CommitObj, error) {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return nil, fmt.Errorf("unmarshal commit: missing header/message separator")
	}
	header := string(data[:idx])
	message := string(data[idx+2:])

	c := &CommitObj{Message: message}
	for _, line := range strings.Split(header, "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("unmarshal commit: malformed header line %q", line)
		}
		switch key {
		case "tree":
			c.TreeHash = Hash(val)
		case "parent":
			c.Parents = append(c.Parents, Hash(val))
		case "author":
			c.Author = val
		case "timestamp":
			ts, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("unmarshal commit: bad timestamp %q: %w", val, err)
			}
			c.Timestamp = ts
		case "signature":
			c.Signature = val
		default:
			return nil, fmt.Errorf("unmarshal commit: unknown header key %q", key)
		}
	}
	return c, nil
}

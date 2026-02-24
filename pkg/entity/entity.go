package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// EntityKind classifies what role an entity plays in a source file.
type EntityKind int

const (
	KindPreamble     EntityKind = iota // Package decl, license headers, file-level comments
	KindImportBlock                    // Import statements grouped together
	KindDeclaration                    // Function, method, class, struct, interface, enum
	KindInterstitial                   // Comments/whitespace between declarations
)

func (k EntityKind) String() string {
	switch k {
	case KindPreamble:
		return "preamble"
	case KindImportBlock:
		return "import_block"
	case KindDeclaration:
		return "declaration"
	case KindInterstitial:
		return "interstitial"
	}
	return fmt.Sprintf("unknown(%d)", int(k))
}

// Entity represents a structural unit within a source file.
type Entity struct {
	Kind      EntityKind
	Name      string // Declaration name (empty for preamble/interstitial)
	DeclKind  string // e.g. "function_definition", "type_definition" (empty for non-declarations)
	Receiver  string // Method receiver (empty for functions/types)
	Signature string // Normalized declaration signature/header text
	Ordinal   int    // Stable ordinal among entities sharing the same base identity

	Body      []byte // Full source bytes of this entity
	BodyHash  string // SHA-256 of Body
	StartByte uint32
	EndByte   uint32
	StartLine int
	EndLine   int

	// For interstitial: identity is relative to neighbors
	PrevEntityKey string
	NextEntityKey string
}

// ComputeHash sets BodyHash from Body content.
func (e *Entity) ComputeHash() {
	h := sha256.Sum256(e.Body)
	e.BodyHash = hex.EncodeToString(h[:])
}

// IdentityKey returns the string used to match this entity across revisions.
func (e *Entity) IdentityKey() string {
	switch e.Kind {
	case KindPreamble:
		return fmt.Sprintf("preamble:%d", e.Ordinal)
	case KindImportBlock:
		return fmt.Sprintf("import_block:%d", e.Ordinal)
	case KindDeclaration:
		sig := normalizeIdentityText(e.Signature)
		return fmt.Sprintf("decl:%s:%s:%s:%s:%d", e.DeclKind, e.Receiver, e.Name, sig, e.Ordinal)
	case KindInterstitial:
		return fmt.Sprintf("between:%s:%s", e.PrevEntityKey, e.NextEntityKey)
	}
	return ""
}

func normalizeIdentityText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return strings.Join(strings.Fields(s), " ")
}

// EntityList is an ordered sequence of entities extracted from a source file.
type EntityList struct {
	Language string
	Path     string
	Source   []byte
	Entities []Entity
}

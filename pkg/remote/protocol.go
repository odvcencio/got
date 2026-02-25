package remote

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

const (
	// ProtocolVersion is the current Got protocol version.
	ProtocolVersion = "1"

	// ClientCapabilities lists all capabilities this client supports.
	ClientCapabilities = "pack,zstd,sideband"

	headerProtocol     = "Got-Protocol"
	headerCapabilities = "Got-Capabilities"
	headerLimits       = "Got-Limits"
)

// ValidateHash checks that a hash is a valid 64-character lowercase hex string (SHA-256).
func ValidateHash(h object.Hash) error {
	s := strings.TrimSpace(string(h))
	if s == "" {
		return fmt.Errorf("hash is empty")
	}
	if len(s) != 64 {
		return fmt.Errorf("hash length %d, expected 64", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("hash contains non-hex characters: %w", err)
	}
	return nil
}

// Capabilities represents a set of protocol capabilities.
type Capabilities struct {
	set map[string]struct{}
}

// ParseCapabilities parses a comma-separated capability string.
func ParseCapabilities(raw string) Capabilities {
	caps := Capabilities{set: make(map[string]struct{})}
	for _, cap := range strings.Split(raw, ",") {
		cap = strings.TrimSpace(cap)
		if cap != "" {
			caps.set[cap] = struct{}{}
		}
	}
	return caps
}

// Has returns true if the capability is present.
func (c Capabilities) Has(name string) bool {
	_, ok := c.set[name]
	return ok
}

// Intersect returns capabilities present in both sets.
func (c Capabilities) Intersect(other Capabilities) Capabilities {
	result := Capabilities{set: make(map[string]struct{})}
	for k := range c.set {
		if _, ok := other.set[k]; ok {
			result.set[k] = struct{}{}
		}
	}
	return result
}

// String returns a sorted comma-separated capability string.
func (c Capabilities) String() string {
	names := make([]string, 0, len(c.set))
	for k := range c.set {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// RemoteError is a structured error from the remote server.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
	Detail  string `json:"detail,omitempty"`
}

func (e *RemoteError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s (%s): %s", e.Message, e.Code, e.Detail)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// ServerLimits holds server-advertised protocol limits parsed from the Got-Limits header.
type ServerLimits struct {
	MaxBatch   int // max objects per batch (0 = use client default)
	MaxPayload int // max payload bytes (0 = use client default)
	MaxObject  int // max single object bytes (0 = use client default)
}

// ParseLimits parses a Got-Limits header value.
// Format: "max_batch=50000,max_payload=67108864,max_object=33554432"
// Unknown keys are ignored. Invalid values are ignored (field stays 0).
func ParseLimits(raw string) ServerLimits {
	var limits ServerLimits
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			continue
		}
		switch key {
		case "max_batch":
			limits.MaxBatch = n
		case "max_payload":
			limits.MaxPayload = n
		case "max_object":
			limits.MaxObject = n
		}
	}
	return limits
}

// tryParseRemoteError attempts to parse a JSON error response body.
func tryParseRemoteError(body []byte) *RemoteError {
	var re RemoteError
	if err := json.Unmarshal(body, &re); err != nil {
		return nil
	}
	if re.Message == "" && re.Code == "" {
		return nil
	}
	return &re
}

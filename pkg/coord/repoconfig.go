package coord

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// RepoCoordConfig holds per-repo coordination configuration.
// Stored as a JSON blob at refs/coord/meta/config.
type RepoCoordConfig struct {
	ConflictMode      string   `json:"conflict_mode"`
	ProtectedEntities []string `json:"protected_entities,omitempty"`
	NotifyOn          []string `json:"notify_on,omitempty"`
	IgnorePatterns    []string `json:"ignore_patterns,omitempty"`
}

const repoConfigRef = "refs/coord/meta/config"

// ErrEntityProtected is returned when an operation targets a protected entity.
var ErrEntityProtected = fmt.Errorf("entity is protected by coordination policy")

// ReadRepoConfig reads the per-repo coordination config from refs/coord/meta/config.
// Returns a default config if none is stored.
func (c *Coordinator) ReadRepoConfig() (*RepoCoordConfig, error) {
	h, err := c.Repo.ResolveRef(repoConfigRef)
	if err != nil {
		// No config stored -- return defaults
		return &RepoCoordConfig{
			ConflictMode: c.Config.ConflictMode,
		}, nil
	}

	var cfg RepoCoordConfig
	if err := c.readJSONBlob(h, &cfg); err != nil {
		return nil, fmt.Errorf("read repo config: %w", err)
	}
	return &cfg, nil
}

// WriteRepoConfig writes the per-repo coordination config to refs/coord/meta/config.
func (c *Coordinator) WriteRepoConfig(cfg *RepoCoordConfig) error {
	h, err := c.writeJSONBlob(cfg)
	if err != nil {
		return fmt.Errorf("write repo config: %w", err)
	}

	oldHash, _ := c.Repo.ResolveRef(repoConfigRef)
	if oldHash == "" {
		return c.Repo.UpdateRefCAS(repoConfigRef, h, object.Hash(""))
	}
	return c.Repo.UpdateRefCAS(repoConfigRef, h, oldHash)
}

// IsEntityProtected checks whether the given entity key matches any of the
// protected entity patterns in the repo config. Uses filepath.Match-like
// semantics but matching against colon-delimited identity keys: '*' matches
// any characters within a single colon-delimited segment, while '**' is not
// supported (use '*' at the end of a pattern to match the rest of the key).
func (c *Coordinator) IsEntityProtected(entityKey string) bool {
	cfg, err := c.ReadRepoConfig()
	if err != nil || len(cfg.ProtectedEntities) == 0 {
		return false
	}

	for _, pattern := range cfg.ProtectedEntities {
		if matchEntityPattern(pattern, entityKey) {
			return true
		}
	}
	return false
}

// matchEntityPattern matches an entity key against a pattern using
// colon-aware glob matching. The pattern and key are split by colons,
// and each segment is matched with simple glob rules:
//   - '*' matches any sequence of non-colon characters
//   - '?' matches exactly one non-colon character
//   - literal characters match exactly
//
// If the pattern has fewer segments than the key and the last pattern
// segment is '*', it matches the remaining key segments.
func matchEntityPattern(pattern, key string) bool {
	// Fast path: exact match
	if pattern == key {
		return true
	}

	patParts := strings.Split(pattern, ":")
	keyParts := strings.Split(key, ":")

	// If pattern ends with a lone *, it matches remaining segments
	if len(patParts) > 0 && patParts[len(patParts)-1] == "*" {
		// Match all segments up to the wildcard
		prefixParts := patParts[:len(patParts)-1]
		if len(keyParts) < len(prefixParts) {
			return false
		}
		for i, pp := range prefixParts {
			if !matchSegment(pp, keyParts[i]) {
				return false
			}
		}
		return true
	}

	// Exact segment count match required
	if len(patParts) != len(keyParts) {
		return false
	}

	for i, pp := range patParts {
		if !matchSegment(pp, keyParts[i]) {
			return false
		}
	}
	return true
}

// matchSegment matches a single segment (no colons) with simple glob rules.
func matchSegment(pattern, s string) bool {
	if pattern == "*" {
		return true
	}

	// Simple glob matching
	px, sx := 0, 0
	starPx, starSx := -1, -1

	for sx < len(s) {
		if px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]) {
			px++
			sx++
		} else if px < len(pattern) && pattern[px] == '*' {
			starPx = px
			starSx = sx
			px++
		} else if starPx >= 0 {
			px = starPx + 1
			starSx++
			sx = starSx
		} else {
			return false
		}
	}

	for px < len(pattern) {
		if pattern[px] != '*' {
			return false
		}
		px++
	}

	return true
}

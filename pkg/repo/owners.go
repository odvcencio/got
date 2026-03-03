package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// OwnerRule maps a pattern (file path or entity key) to a set of owners.
type OwnerRule struct {
	Pattern  string
	Owners   []string
	IsEntity bool // true if the pattern matches entity keys (e.g., "func:*Handler")

	// Internal matching fields.
	hasSlash bool           // path pattern contains a slash
	regex    *regexp.Regexp // compiled regex for ** or * patterns
}

// OwnersFile holds the parsed rules from a .graftowners file.
type OwnersFile struct {
	Rules []OwnerRule
}

// entityPrefixes lists the known entity kind prefixes that identify entity
// patterns in .graftowners. A pattern starting with one of these followed by
// a colon is treated as an entity pattern (e.g., "func:*Handler").
var entityPrefixes = []string{
	"func:",
	"type:",
	"struct:",
	"interface:",
	"method:",
	"const:",
	"var:",
	"class:",
	"enum:",
	"trait:",
	"module:",
	"declaration:",
}

// isEntityPattern returns true if the pattern matches an entity key format.
// Entity patterns have the form "<kind>:<name-pattern>".
func isEntityPattern(pattern string) bool {
	for _, prefix := range entityPrefixes {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
	}
	return false
}

// ParseOwnersFile parses .graftowners content from raw bytes.
//
// Format:
//
//	# comment
//	<pattern>  <owner1> [<owner2> ...]
//
// Entity patterns have the form "func:*Handler" or "type:Config*".
// Path patterns use glob syntax: "pkg/auth/**", "*.go", etc.
func ParseOwnersFile(data []byte) (*OwnersFile, error) {
	of := &OwnersFile{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			// A line with only a pattern and no owners is invalid; skip.
			continue
		}

		pattern := fields[0]
		owners := fields[1:]

		rule := OwnerRule{
			Pattern:  pattern,
			Owners:   owners,
			IsEntity: isEntityPattern(pattern),
		}

		if !rule.IsEntity {
			rule.hasSlash = strings.Contains(pattern, "/")
			// Compile regex for patterns with ** or * for flexible matching.
			if strings.Contains(pattern, "**") {
				if re, err := regexp.Compile(globToRegex(pattern)); err == nil {
					rule.regex = re
				}
			}
		}

		of.Rules = append(of.Rules, rule)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return of, nil
}

// OwnersFor returns all unique owners that match the given path and/or entity key.
// Path patterns are matched against the file path. Entity patterns are matched
// against the entity key (e.g., "func:LoginHandler"). Both types of matches
// accumulate; duplicates are removed while preserving order.
func (o *OwnersFile) OwnersFor(path, entityKey string) []string {
	if o == nil || len(o.Rules) == 0 {
		return nil
	}

	path = filepath.ToSlash(path)

	var owners []string
	seen := make(map[string]bool)

	for _, rule := range o.Rules {
		matched := false

		if rule.IsEntity {
			// Match entity key against the entity pattern.
			if entityKey != "" {
				matched = matchEntityPattern(rule.Pattern, entityKey)
			}
		} else {
			// Match path against the file pattern.
			if path != "" {
				matched = matchPathPattern(&rule, path)
			}
		}

		if matched {
			for _, owner := range rule.Owners {
				if !seen[owner] {
					seen[owner] = true
					owners = append(owners, owner)
				}
			}
		}
	}

	return owners
}

// matchEntityPattern checks if an entity key matches an entity pattern.
// The pattern has the form "kind:nameGlob" and the entity key has the form
// "kind:name". The kind must match exactly, and the name portion supports
// glob wildcards (* and ?).
func matchEntityPattern(pattern, entityKey string) bool {
	// Split pattern into kind and name-glob.
	pColon := strings.Index(pattern, ":")
	if pColon < 0 {
		return false
	}
	pKind := pattern[:pColon]
	pName := pattern[pColon+1:]

	// Split entity key into kind and name.
	eColon := strings.Index(entityKey, ":")
	if eColon < 0 {
		return false
	}
	eKind := entityKey[:eColon]
	eName := entityKey[eColon+1:]

	// Kind must match exactly.
	if pKind != eKind {
		return false
	}

	// Name portion supports glob matching.
	matched, _ := filepath.Match(pName, eName)
	return matched
}

// matchPathPattern checks if a file path matches a path-based owner rule.
func matchPathPattern(rule *OwnerRule, path string) bool {
	pattern := rule.Pattern

	// Handle trailing slash as directory prefix match.
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern) || strings.HasPrefix(path+"/", pattern)
	}

	// Use compiled regex for ** patterns.
	if rule.regex != nil {
		return rule.regex.MatchString(path)
	}

	// Pattern contains a slash: match against the full relative path.
	if rule.hasSlash {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Pattern without a slash: match against the filename component only.
	base := filepath.Base(path)
	matched, _ := filepath.Match(pattern, base)
	return matched
}

// ReadOwnersFile loads and parses .graftowners from the repository root.
// Returns an empty OwnersFile if the file does not exist.
func (r *Repo) ReadOwnersFile() (*OwnersFile, error) {
	ownerPath := filepath.Join(r.RootDir, ".graftowners")
	data, err := os.ReadFile(ownerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &OwnersFile{}, nil
		}
		return nil, err
	}
	return ParseOwnersFile(data)
}

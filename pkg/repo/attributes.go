package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Attributes holds the parsed attribute rules from a .graftattributes file.
type Attributes struct {
	Rules []AttributeRule
}

// AttributeRule maps a file pattern to a set of key-value attributes.
type AttributeRule struct {
	Pattern string
	Attrs   map[string]string // key -> value (boolean attrs have value "true")

	// Internal matching fields.
	hasSlash bool           // pattern contains a slash, match against full path
	regex    *regexp.Regexp // compiled regex for ** patterns
}

// ReadAttributes loads .graftattributes from the repo root.
// Returns an empty Attributes if the file doesn't exist.
func (r *Repo) ReadAttributes() (*Attributes, error) {
	attrs := &Attributes{}

	attrPath := filepath.Join(r.RootDir, ".graftattributes")
	f, err := os.Open(attrPath)
	if err != nil {
		if os.IsNotExist(err) {
			return attrs, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		rule := parseAttributeLine(line)
		if rule != nil {
			attrs.Rules = append(attrs.Rules, *rule)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return attrs, nil
}

// ParseAttributes parses .graftattributes content from a string.
// Useful for testing or reading attributes from non-file sources.
func ParseAttributes(content string) *Attributes {
	attrs := &Attributes{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		rule := parseAttributeLine(line)
		if rule != nil {
			attrs.Rules = append(attrs.Rules, *rule)
		}
	}
	return attrs
}

// parseAttributeLine parses a single line from a .graftattributes file.
// Returns nil if the line is empty or a comment.
//
// Format: <pattern> <attr1>[=<value>] [<attr2>[=<value>]] ...
// Special: "binary" is shorthand for "-diff -merge".
// Attributes prefixed with "-" set the value to "false".
// Attributes without =value are boolean (value "true").
func parseAttributeLine(line string) *AttributeRule {
	// Trim whitespace.
	line = strings.TrimSpace(line)

	// Empty lines are skipped.
	if line == "" {
		return nil
	}

	// Comment lines are skipped.
	if strings.HasPrefix(line, "#") {
		return nil
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return nil
	}

	pattern := fields[0]
	rule := &AttributeRule{
		Pattern:  pattern,
		Attrs:    make(map[string]string),
		hasSlash: strings.Contains(pattern, "/"),
	}

	// Compile regex for ** patterns (reuse globToRegex from ignore.go).
	if strings.Contains(pattern, "**") {
		if re, err := regexp.Compile(globToRegex(pattern)); err == nil {
			rule.regex = re
		}
	}

	for _, attr := range fields[1:] {
		if attr == "binary" {
			// "binary" is shorthand for -diff -merge.
			rule.Attrs["diff"] = "false"
			rule.Attrs["merge"] = "false"
			continue
		}

		if strings.HasPrefix(attr, "-") {
			// Negated attribute: -diff means diff=false.
			rule.Attrs[attr[1:]] = "false"
			continue
		}

		if idx := strings.Index(attr, "="); idx >= 0 {
			// Key=value attribute.
			key := attr[:idx]
			value := attr[idx+1:]
			rule.Attrs[key] = value
		} else {
			// Boolean attribute (present = true).
			rule.Attrs[attr] = "true"
		}
	}

	return rule
}

// Match returns all attributes that apply to the given path.
// Later rules override earlier ones for the same attribute key.
// The path should use forward slashes and be relative to the repository root.
func (a *Attributes) Match(path string) map[string]string {
	result := make(map[string]string)
	path = filepath.ToSlash(path)

	for _, rule := range a.Rules {
		if rule.matchPath(path) {
			for k, v := range rule.Attrs {
				result[k] = v
			}
		}
	}

	return result
}

// matchPath checks if the given relative path matches this rule's pattern.
func (r *AttributeRule) matchPath(path string) bool {
	if r.hasSlash {
		// Pattern contains a slash: match against the full relative path.
		return r.matchString(path)
	}

	// Pattern without a slash: match against the filename component only.
	base := filepath.Base(path)
	return r.matchString(base)
}

// matchString performs the actual pattern match against a target string.
func (r *AttributeRule) matchString(target string) bool {
	if r.regex != nil {
		return r.regex.MatchString(target)
	}
	matched, _ := filepath.Match(r.Pattern, target)
	return matched
}

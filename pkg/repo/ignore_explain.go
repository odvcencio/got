package repo

import "path/filepath"

// IgnoreMatch describes one ignore rule that matched a path.
type IgnoreMatch struct {
	Pattern       string `json:"pattern"`
	Source        string `json:"source,omitempty"`
	Line          int    `json:"line,omitempty"`
	Negated       bool   `json:"negated,omitempty"`
	DirectoryOnly bool   `json:"directoryOnly,omitempty"`
	Rooted        bool   `json:"rooted,omitempty"`
}

// IgnoreExplanation captures the full ignore decision for a path.
type IgnoreExplanation struct {
	Path    string        `json:"path"`
	Ignored bool          `json:"ignored"`
	Final   *IgnoreMatch  `json:"final,omitempty"`
	Matches []IgnoreMatch `json:"matches,omitempty"`
}

// Explain reports which ignore rules matched the given repo-relative path.
// Rules are returned in evaluation order, and the final match determines
// whether the path is ignored.
func (ic *IgnoreChecker) Explain(path string) IgnoreExplanation {
	path = filepath.ToSlash(path)

	result := IgnoreExplanation{Path: path}
	for _, pattern := range ic.patterns {
		if !pattern.matches(path) {
			continue
		}

		match := IgnoreMatch{
			Pattern:       pattern.original,
			Source:        pattern.source,
			Line:          pattern.line,
			Negated:       pattern.negated,
			DirectoryOnly: pattern.dirOnly,
			Rooted:        pattern.rooted,
		}
		if match.Pattern == "" {
			match.Pattern = pattern.pattern
		}

		result.Matches = append(result.Matches, match)
		result.Ignored = !pattern.negated
		result.Final = &result.Matches[len(result.Matches)-1]
	}

	return result
}

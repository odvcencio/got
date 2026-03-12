package gotreesitter

import (
	"fmt"
	"strings"
)

// InjectionResult holds parse results for a multi-language document.
type InjectionResult struct {
	// Tree is the parent language's parse tree.
	Tree *Tree
	// Injections contains child language parse results, ordered by position.
	Injections []Injection
}

// Injection is a single embedded language region.
type Injection struct {
	// Language is the detected language name (e.g., "javascript").
	Language string
	// Tree is the parse tree for this region, or nil if the language
	// was not registered.
	Tree *Tree
	// Ranges are the source ranges this tree covers.
	Ranges []Range
	// Node is the parent tree node that triggered the injection.
	Node *Node
}

// InjectionParser parses documents with embedded languages.
//
// InjectionParser is not safe for concurrent use. It caches child parsers and
// mutates shared maps during parse operations.
type InjectionParser struct {
	// languages maps language name -> Language.
	languages map[string]*Language
	// injectionQueries maps parent language name -> compiled injection query.
	injectionQueries map[string]*Query
	// parsers caches Parser instances per language for reuse.
	parsers map[string]*Parser
	// maxDepth limits nested injection recursion. Zero means use default.
	maxDepth int
}

// NewInjectionParser creates an InjectionParser.
func NewInjectionParser() *InjectionParser {
	return &InjectionParser{
		languages:        make(map[string]*Language),
		injectionQueries: make(map[string]*Query),
		parsers:          make(map[string]*Parser),
	}
}

// RegisterLanguage adds a language that can be used as parent or child.
func (ip *InjectionParser) RegisterLanguage(name string, lang *Language) {
	ip.languages[name] = lang
}

// RegisterInjectionQuery sets the injection query for a parent language.
// The query should use @injection.content and #set! injection.language
// conventions. It is compiled against the registered parent language.
func (ip *InjectionParser) RegisterInjectionQuery(parentLang string, query string) error {
	lang, ok := ip.languages[parentLang]
	if !ok {
		return fmt.Errorf("injection: parent language %q not registered", parentLang)
	}
	q, err := NewQuery(query, lang)
	if err != nil {
		return fmt.Errorf("injection: compiling query for %q: %w", parentLang, err)
	}
	ip.injectionQueries[parentLang] = q
	return nil
}

// Parse parses source as parentLang, then recursively parses injected regions.
func (ip *InjectionParser) Parse(source []byte, parentLang string) (*InjectionResult, error) {
	lang, ok := ip.languages[parentLang]
	if !ok {
		return nil, fmt.Errorf("injection: language %q not registered", parentLang)
	}

	parser := ip.getParser(parentLang, lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("injection: parsing %q: %w", parentLang, err)
	}

	injections, err := ip.findAndParseInjections(source, parentLang, tree, 0)
	if err != nil {
		return nil, err
	}

	return &InjectionResult{
		Tree:       tree,
		Injections: injections,
	}, nil
}

// ParseIncremental re-parses after edits, reusing unchanged child trees.
func (ip *InjectionParser) ParseIncremental(source []byte, parentLang string,
	oldResult *InjectionResult) (*InjectionResult, error) {

	lang, ok := ip.languages[parentLang]
	if !ok {
		return nil, fmt.Errorf("injection: language %q not registered", parentLang)
	}

	parser := ip.getParser(parentLang, lang)
	newTree, err := parser.ParseIncremental(source, oldResult.Tree)
	if err != nil {
		return nil, fmt.Errorf("injection: incremental parsing %q: %w", parentLang, err)
	}

	// Determine which ranges changed between old and new parent trees.
	changedRanges := DiffChangedRanges(oldResult.Tree, newTree)

	// Re-detect injections from the new parent tree.
	newDetected, err := ip.detectInjections(source, parentLang, newTree)
	if err != nil {
		return nil, err
	}

	// For each detected injection, check if it overlaps a changed range.
	// If not, try to reuse the old child tree.
	var injections []Injection
	for _, det := range newDetected {
		if det.Language == "" {
			injections = append(injections, det)
			continue
		}

		childLang, hasLang := ip.languages[det.Language]
		if !hasLang {
			injections = append(injections, det)
			continue
		}

		// Check if this injection's ranges overlap any changed range.
		changed := false
		for _, cr := range changedRanges {
			for _, r := range det.Ranges {
				if r.StartByte < cr.EndByte && r.EndByte > cr.StartByte {
					changed = true
					break
				}
			}
			if changed {
				break
			}
		}

		if !changed {
			// Try to reuse old child tree.
			if oldChild := ip.findOldInjection(oldResult, det.Language, det.Ranges); oldChild != nil {
				det.Tree = oldChild
				injections = append(injections, det)
				continue
			}
		}

		// Parse (or reparse) this injection region.
		childParser := ip.getParser(det.Language, childLang)
		childParser.SetIncludedRanges(det.Ranges)
		childTree, err := childParser.Parse(source)
		if err != nil {
			// If child parse fails, record injection without tree.
			injections = append(injections, det)
			continue
		}
		det.Tree = childTree
		injections = append(injections, det)
	}

	return &InjectionResult{
		Tree:       newTree,
		Injections: injections,
	}, nil
}

// defaultMaxInjectionDepth limits recursion to prevent infinite loops.
const defaultMaxInjectionDepth = 10

// SetMaxDepth overrides the nested injection recursion limit.
// Depth values <= 0 restore the default limit.
func (ip *InjectionParser) SetMaxDepth(depth int) {
	if ip == nil {
		return
	}
	if depth <= 0 {
		ip.maxDepth = 0
		return
	}
	ip.maxDepth = depth
}

func (ip *InjectionParser) effectiveMaxDepth() int {
	if ip == nil || ip.maxDepth <= 0 {
		return defaultMaxInjectionDepth
	}
	return ip.maxDepth
}

// findAndParseInjections detects injections in tree and parses them, recursing.
func (ip *InjectionParser) findAndParseInjections(source []byte, parentLang string,
	tree *Tree, depth int) ([]Injection, error) {

	if depth >= ip.effectiveMaxDepth() {
		return nil, nil
	}

	detected, err := ip.detectInjections(source, parentLang, tree)
	if err != nil {
		return nil, err
	}

	var result []Injection
	for _, det := range detected {
		if det.Language == "" {
			result = append(result, det)
			continue
		}

		childLang, ok := ip.languages[det.Language]
		if !ok {
			// Language not registered — record injection without tree.
			result = append(result, det)
			continue
		}

		childParser := ip.getParser(det.Language, childLang)
		childParser.SetIncludedRanges(det.Ranges)
		childTree, err := childParser.Parse(source)
		if err != nil {
			result = append(result, det)
			continue
		}
		det.Tree = childTree

		// Recurse: check if this child language has injection queries too.
		if _, hasQuery := ip.injectionQueries[det.Language]; hasQuery {
			nested, err := ip.findAndParseInjections(source, det.Language, childTree, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, det)
			result = append(result, nested...)
		} else {
			result = append(result, det)
		}
	}

	return result, nil
}

// detectInjections runs the injection query for parentLang against tree
// and returns detected injection regions (without parsing them).
func (ip *InjectionParser) detectInjections(source []byte, parentLang string,
	tree *Tree) ([]Injection, error) {

	q, ok := ip.injectionQueries[parentLang]
	if !ok {
		return nil, nil
	}

	lang := ip.languages[parentLang]
	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}

	cursor := q.Exec(root, lang, source)

	// Collect injection regions grouped by language.
	type injectionEntry struct {
		language string
		ranges   []Range
		node     *Node
	}
	var entries []injectionEntry

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var contentNode *Node
		var langName string

		// Check for static language from #set! injection.language.
		if vals := match.SetValues(q, "injection.language"); len(vals) > 0 {
			langName = vals[0]
		}

		for _, cap := range match.Captures {
			switch cap.Name {
			case "injection.content":
				contentNode = cap.Node
			case "injection.language":
				// Dynamic language detection: node text is the language name.
				if langName == "" && cap.Node != nil {
					langName = strings.TrimSpace(cap.Node.Text(source))
				}
			}
		}

		if contentNode == nil {
			continue
		}

		entries = append(entries, injectionEntry{
			language: langName,
			ranges:   []Range{contentNode.Range()},
			node:     contentNode,
		})
	}

	// Convert entries to Injection structs.
	injections := make([]Injection, len(entries))
	for i, e := range entries {
		injections[i] = Injection{
			Language: e.language,
			Ranges:   e.ranges,
			Node:     e.node,
		}
	}

	return injections, nil
}

// findOldInjection searches oldResult for a matching injection by language and ranges.
func (ip *InjectionParser) findOldInjection(oldResult *InjectionResult, lang string, ranges []Range) *Tree {
	for _, old := range oldResult.Injections {
		if old.Language != lang || old.Tree == nil || len(old.Ranges) != len(ranges) {
			continue
		}
		match := true
		for i, r := range old.Ranges {
			if r != ranges[i] {
				match = false
				break
			}
		}
		if match {
			return old.Tree
		}
	}
	return nil
}

// getParser returns a cached Parser for the language, creating one if needed.
func (ip *InjectionParser) getParser(name string, lang *Language) *Parser {
	if p, ok := ip.parsers[name]; ok {
		// Reset included ranges for fresh use.
		p.SetIncludedRanges(nil)
		return p
	}
	p := NewParser(lang)
	ip.parsers[name] = p
	return p
}

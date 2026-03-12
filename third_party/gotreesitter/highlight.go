package gotreesitter

import (
	"fmt"
	"sort"
)

// HighlightRange represents a styled range of source code, mapping a byte span
// to a capture name from a highlight query. The editor maps capture names
// (e.g., "keyword", "string", "function") to FSS style classes.
type HighlightRange struct {
	StartByte uint32
	EndByte   uint32
	Capture   string // "keyword", "string", "function", etc.
}

// Highlighter is a high-level API that takes source code and returns styled
// ranges. It combines a Parser, a compiled Query, and a Language to provide
// a single Highlight() call for the editor.
type Highlighter struct {
	parser             *Parser
	query              *Query
	lang               *Language
	tokenSourceFactory func(source []byte) TokenSource
	injectionQuery     *Query
	injectionResolver  HighlighterInjectionResolver
	childQueries       map[string]*Query
}

// HighlighterOption configures a Highlighter.
type HighlighterOption func(*Highlighter)

// WithTokenSourceFactory sets a factory function that creates a TokenSource
// for each Highlight call. This is needed for languages that use a custom
// lexer bridge (like Go, which uses go/scanner instead of a DFA lexer).
//
// When set, Highlight() calls ParseWithTokenSource instead of Parse.
func WithTokenSourceFactory(factory func(source []byte) TokenSource) HighlighterOption {
	return func(h *Highlighter) {
		h.tokenSourceFactory = factory
	}
}

// NewHighlighter creates a Highlighter for the given language and highlight
// query (in tree-sitter .scm format). Returns an error if the query fails
// to compile.
func NewHighlighter(lang *Language, highlightQuery string, opts ...HighlighterOption) (*Highlighter, error) {
	q, err := NewQuery(highlightQuery, lang)
	if err != nil {
		return nil, err
	}

	h := &Highlighter{
		parser: NewParser(lang),
		query:  q,
		lang:   lang,
	}
	for _, opt := range opts {
		opt(h)
	}
	if lang != nil {
		if spec, ok := lookupHighlighterInjection(lang.Name); ok {
			injQ, injErr := NewQuery(spec.Query, lang)
			if injErr != nil {
				return nil, fmt.Errorf("highlighter injection query for %q: %w", lang.Name, injErr)
			}
			h.injectionQuery = injQ
			h.injectionResolver = spec.ResolveLanguage
			h.childQueries = make(map[string]*Query)
		}
	}
	return h, nil
}

// HighlightIncremental re-highlights source after edits were applied to oldTree.
// Returns the new highlight ranges and the new parse tree (for use in subsequent
// incremental calls). Call oldTree.Edit() before calling this.
func (h *Highlighter) HighlightIncremental(source []byte, oldTree *Tree) ([]HighlightRange, *Tree) {
	if len(source) == 0 {
		return nil, NewTree(nil, source, h.lang)
	}

	tree := h.parse(source, oldTree)

	if tree.RootNode() == nil {
		return nil, tree
	}

	return h.highlightTree(tree, source), tree
}

// Highlight parses the source code and executes the highlight query, returning
// a slice of HighlightRange sorted by StartByte. When ranges overlap, inner
// (more specific) captures take priority over outer ones.
func (h *Highlighter) Highlight(source []byte) []HighlightRange {
	if len(source) == 0 {
		return nil
	}

	tree := h.parse(source, nil)
	if tree == nil || tree.RootNode() == nil {
		if tree != nil {
			tree.Release()
		}
		return nil
	}
	defer tree.Release()

	return h.highlightTree(tree, source)
}

func (h *Highlighter) parse(source []byte, oldTree *Tree) *Tree {
	return dispatchParse(h.parser, source, oldTree, h.tokenSourceFactory, h.lang)
}

func (h *Highlighter) highlightTree(tree *Tree, source []byte) []HighlightRange {
	matches := h.query.Execute(tree)
	if len(matches) == 0 && h.injectionQuery == nil {
		return nil
	}

	var ranges []HighlightRange
	for _, m := range matches {
		for _, c := range m.Captures {
			node := c.Node
			if node.StartByte() == node.EndByte() {
				continue
			}
			ranges = append(ranges, HighlightRange{
				StartByte: node.StartByte(),
				EndByte:   node.EndByte(),
				Capture:   c.Name,
			})
		}
	}

	ranges = h.appendInjectedRanges(tree, source, ranges)

	if len(ranges) == 0 {
		return nil
	}

	return resolveOverlaps(ranges)
}

// resolveOverlaps takes a range list (in any order) and returns a sorted,
// non-overlapping slice where inner (narrower) captures take priority over
// outer (wider) ones.
//
// Algorithm:
//  1. Sort ranges by start asc, width desc.
//  2. Sweep boundaries with a stack of active nested ranges.
//     The top of the stack is the currently active innermost capture.
//
// This avoids the previous second O(n log n) event sort.
func resolveOverlaps(ranges []HighlightRange) []HighlightRange {
	if len(ranges) == 0 {
		return nil
	}

	sorted := make([]HighlightRange, 0, len(ranges))
	for i := range ranges {
		r := ranges[i]
		if r.EndByte > r.StartByte {
			sorted = append(sorted, r)
		}
	}
	if len(sorted) == 0 {
		return nil
	}

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartByte != sorted[j].StartByte {
			return sorted[i].StartByte < sorted[j].StartByte
		}
		wi := sorted[i].EndByte - sorted[i].StartByte
		wj := sorted[j].EndByte - sorted[j].StartByte
		return wi > wj
	})

	var stack []HighlightRange
	var result []HighlightRange
	emit := func(start, end uint32, capture string) {
		if capture == "" || end <= start {
			return
		}
		if n := len(result); n > 0 && result[n-1].Capture == capture && result[n-1].EndByte == start {
			result[n-1].EndByte = end
			return
		}
		result = append(result, HighlightRange{StartByte: start, EndByte: end, Capture: capture})
	}

	curPos := sorted[0].StartByte
	nextStartIdx := 0

	for nextStartIdx < len(sorted) || len(stack) > 0 {
		nextStartPos := ^uint32(0)
		if nextStartIdx < len(sorted) {
			nextStartPos = sorted[nextStartIdx].StartByte
		}
		nextEndPos := ^uint32(0)
		if len(stack) > 0 {
			nextEndPos = stack[len(stack)-1].EndByte
		}
		nextPos := nextStartPos
		if nextEndPos < nextPos {
			nextPos = nextEndPos
		}

		if len(stack) > 0 && nextPos > curPos {
			emit(curPos, nextPos, stack[len(stack)-1].Capture)
			curPos = nextPos
		} else if curPos < nextPos {
			curPos = nextPos
		}

		// End events at this boundary are processed before start events.
		for len(stack) > 0 && stack[len(stack)-1].EndByte <= curPos {
			stack = stack[:len(stack)-1]
		}
		for nextStartIdx < len(sorted) && sorted[nextStartIdx].StartByte == curPos {
			stack = append(stack, sorted[nextStartIdx])
			nextStartIdx++
		}

		// Skip forward to the next start when no capture is active.
		if len(stack) == 0 && nextStartIdx < len(sorted) && curPos < sorted[nextStartIdx].StartByte {
			curPos = sorted[nextStartIdx].StartByte
		}
		if len(stack) == 0 && nextStartIdx >= len(sorted) {
			break
		}
		if len(stack) > 0 && curPos < stack[len(stack)-1].StartByte {
			curPos = stack[len(stack)-1].StartByte
		}
		if len(stack) > 0 && curPos > stack[len(stack)-1].EndByte {
			for len(stack) > 0 && curPos >= stack[len(stack)-1].EndByte {
				stack = stack[:len(stack)-1]
			}
		}
	}

	return result
}

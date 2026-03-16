package grammars

import (
	"fmt"
	"sync"

	"github.com/odvcencio/gotreesitter"
)

// ParseFile detects the language from filename, parses source, and returns
// a BoundTree. The caller must call Release() on the returned BoundTree.
func ParseFile(filename string, source []byte) (*gotreesitter.BoundTree, error) {
	entry := DetectLanguage(filename)
	if entry == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filename)
	}

	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)

	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		tree, err = parser.ParseWithTokenSource(source, ts)
	} else {
		tree, err = parser.Parse(source)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	return gotreesitter.Bind(tree), nil
}

// parserPools holds one ParserPool per language (keyed by lang pointer).
var (
	parserPoolsMu sync.Mutex
	parserPools   = map[*gotreesitter.Language]*gotreesitter.ParserPool{}
)

func getOrCreatePool(lang *gotreesitter.Language) *gotreesitter.ParserPool {
	parserPoolsMu.Lock()
	defer parserPoolsMu.Unlock()
	if pp, ok := parserPools[lang]; ok {
		return pp
	}
	pp := gotreesitter.NewParserPool(lang)
	parserPools[lang] = pp
	return pp
}

// ParseFilePooled is like ParseFile but reuses parser instances via a per-language
// sync.Pool, reducing allocations for repeated parsing of the same language.
// The caller must call Release() on the returned BoundTree.
func ParseFilePooled(filename string, source []byte) (*gotreesitter.BoundTree, error) {
	entry := DetectLanguage(filename)
	if entry == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filename)
	}

	lang := entry.Language()
	pp := getOrCreatePool(lang)

	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		tree, err = pp.ParseWithTokenSource(source, ts)
	} else {
		tree, err = pp.Parse(source)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	return gotreesitter.Bind(tree), nil
}

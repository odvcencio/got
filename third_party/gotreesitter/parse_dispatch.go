package gotreesitter

// dispatchParse is a best-effort parse helper for highlighter/tagger flows.
// On parser error it intentionally returns an empty tree so editor features
// degrade gracefully instead of failing hard.
func dispatchParse(p *Parser, source []byte, oldTree *Tree, tsFactory func([]byte) TokenSource, lang *Language) *Tree {
	var tree *Tree
	var err error
	if tsFactory != nil {
		ts := tsFactory(source)
		if oldTree != nil {
			tree, err = p.ParseIncrementalWithTokenSource(source, oldTree, ts)
		} else {
			tree, err = p.ParseWithTokenSource(source, ts)
		}
	} else if oldTree != nil {
		tree, err = p.ParseIncremental(source, oldTree)
	} else {
		tree, err = p.Parse(source)
	}
	if err != nil {
		return NewTree(nil, source, lang)
	}
	return tree
}

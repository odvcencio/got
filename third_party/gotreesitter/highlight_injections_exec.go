package gotreesitter

import "strings"

func (h *Highlighter) appendInjectedRanges(tree *Tree, source []byte, ranges []HighlightRange) []HighlightRange {
	if h == nil || tree == nil || h.injectionQuery == nil || h.injectionResolver == nil || len(source) == 0 {
		return ranges
	}
	root := tree.RootNode()
	if root == nil {
		return ranges
	}

	cursor := h.injectionQuery.Exec(root, h.lang, source)
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var contentNode *Node
		var startNode *Node
		langHint := ""
		if vals := match.SetValues(h.injectionQuery, "injection.language"); len(vals) > 0 {
			langHint = vals[0]
		}
		for _, c := range match.Captures {
			switch c.Name {
			case "injection.content":
				contentNode = c.Node
			case "injection.start":
				startNode = c.Node
			case "injection.language":
				if langHint == "" && c.Node != nil {
					langHint = c.Node.Text(source)
				}
			}
		}
		if contentNode == nil {
			continue
		}

		normalizedHint := normalizeInjectionLanguageHint(langHint)
		if normalizedHint == "" {
			continue
		}

		childLang, childQueryStr, childTSFactory, ok := h.injectionResolver(normalizedHint)
		if !ok || childLang == nil || strings.TrimSpace(childQueryStr) == "" {
			continue
		}

		start := contentNode.StartByte()
		if startNode != nil && startNode.StartByte() < start {
			start = startNode.StartByte()
		}
		end := contentNode.EndByte()
		if end <= start || int(end) > len(source) {
			continue
		}
		childSource := source[start:end]

		childParser := NewParser(childLang)
		var childTree *Tree
		var err error
		if childTSFactory != nil {
			childTree, err = childParser.ParseWithTokenSource(childSource, childTSFactory(childSource))
		} else {
			childTree, err = childParser.Parse(childSource)
		}
		if err != nil || childTree == nil || childTree.RootNode() == nil {
			if childTree != nil {
				childTree.Release()
			}
			continue
		}

		cacheKey := childLang.Name
		if cacheKey == "" {
			cacheKey = normalizedHint
		}
		childQuery := h.childQueries[cacheKey]
		if childQuery == nil {
			childQuery, err = NewQuery(childQueryStr, childLang)
			if err != nil {
				childTree.Release()
				continue
			}
			h.childQueries[cacheKey] = childQuery
		}

		childRanges := collectHighlightRanges(childQuery, childTree)
		childTree.Release()

		if len(childRanges) == 0 && cacheKey == "go" {
			const prefix = "package main\nfunc __gts_markdown_fence__() {\n"
			const suffix = "\n}\n"
			wrapped := make([]byte, 0, len(prefix)+len(childSource)+len(suffix))
			wrapped = append(wrapped, []byte(prefix)...)
			wrapped = append(wrapped, childSource...)
			wrapped = append(wrapped, []byte(suffix)...)

			wrappedTree, wrappedErr := h.parseInjectedTree(childLang, childTSFactory, wrapped)
			if wrappedErr == nil && wrappedTree != nil && wrappedTree.RootNode() != nil {
				offset := uint32(len(prefix))
				endOffset := offset + uint32(len(childSource))
				for _, r := range collectHighlightRanges(childQuery, wrappedTree) {
					if r.StartByte < offset || r.EndByte > endOffset {
						continue
					}
					childRanges = append(childRanges, HighlightRange{
						StartByte: r.StartByte - offset,
						EndByte:   r.EndByte - offset,
						Capture:   r.Capture,
					})
				}
				wrappedTree.Release()
			} else if wrappedTree != nil {
				wrappedTree.Release()
			}
		}

		for _, r := range childRanges {
			ranges = append(ranges, HighlightRange{
				StartByte: r.StartByte + start,
				EndByte:   r.EndByte + start,
				Capture:   r.Capture,
			})
		}
	}

	return ranges
}

func (h *Highlighter) parseInjectedTree(lang *Language, tokenSourceFactory func(source []byte) TokenSource, source []byte) (*Tree, error) {
	childParser := NewParser(lang)
	if tokenSourceFactory != nil {
		return childParser.ParseWithTokenSource(source, tokenSourceFactory(source))
	}
	return childParser.Parse(source)
}

func collectHighlightRanges(q *Query, tree *Tree) []HighlightRange {
	if q == nil || tree == nil {
		return nil
	}
	matches := q.Execute(tree)
	if len(matches) == 0 {
		return nil
	}
	ranges := make([]HighlightRange, 0, len(matches)*2)
	for _, m := range matches {
		for _, c := range m.Captures {
			if c.Node.StartByte() == c.Node.EndByte() {
				continue
			}
			ranges = append(ranges, HighlightRange{
				StartByte: c.Node.StartByte(),
				EndByte:   c.Node.EndByte(),
				Capture:   c.Name,
			})
		}
	}
	return ranges
}

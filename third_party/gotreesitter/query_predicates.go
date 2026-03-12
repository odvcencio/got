package gotreesitter

import "strings"

func (q *Query) matchesPredicates(predicates []QueryPredicate, captures []QueryCapture, lang *Language, source []byte) bool {
	if len(predicates) == 0 {
		return true
	}

	for _, pred := range predicates {
		switch pred.kind {
		case predicateEq:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			right := pred.literal
			if pred.rightCapture != "" {
				var okRight bool
				right, okRight = captureText(pred.rightCapture, captures, source)
				if !okRight {
					return false
				}
			}
			if left != right {
				return false
			}

		case predicateNotEq:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			right := pred.literal
			if pred.rightCapture != "" {
				var okRight bool
				right, okRight = captureText(pred.rightCapture, captures, source)
				if !okRight {
					return false
				}
			}
			if left == right {
				return false
			}

		case predicateMatch:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			if pred.regex == nil || !pred.regex.MatchString(left) {
				return false
			}

		case predicateNotMatch:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			if pred.regex != nil && pred.regex.MatchString(left) {
				return false
			}

		case predicateAnyEq:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			right := pred.literal
			if pred.rightCapture != "" {
				var ok bool
				right, ok = captureText(pred.rightCapture, captures, source)
				if !ok {
					return false
				}
			}
			found := false
			for _, n := range nodes {
				if n.Text(source) == right {
					found = true
					break
				}
			}
			if !found {
				return false
			}

		case predicateAnyNotEq:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			right := pred.literal
			if pred.rightCapture != "" {
				var ok bool
				right, ok = captureText(pred.rightCapture, captures, source)
				if !ok {
					return false
				}
			}
			found := false
			for _, n := range nodes {
				if n.Text(source) != right {
					found = true
					break
				}
			}
			if !found {
				return false
			}

		case predicateAnyMatch:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			if pred.regex == nil {
				return false
			}
			found := false
			for _, n := range nodes {
				if pred.regex.MatchString(n.Text(source)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}

		case predicateAnyNotMatch:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			if pred.regex == nil {
				return false
			}
			found := false
			for _, n := range nodes {
				if !pred.regex.MatchString(n.Text(source)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}

		case predicateLuaMatch:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			if pred.regex == nil || !pred.regex.MatchString(left) {
				return false
			}

		case predicateAnyOf:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			matched := false
			for _, v := range pred.values {
				if left == v {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}

		case predicateNotAnyOf:
			left, ok := captureText(pred.leftCapture, captures, source)
			if !ok {
				return false
			}
			for _, v := range pred.values {
				if left == v {
					return false
				}
			}

		case predicateHasAncestor:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			hasAny := false
			for _, n := range nodes {
				if nodeHasAncestorType(n, pred.values, lang) {
					hasAny = true
					break
				}
			}
			if !hasAny {
				return false
			}

		case predicateNotHasAncestor:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			for _, n := range nodes {
				if nodeHasAncestorType(n, pred.values, lang) {
					return false
				}
			}

		case predicateNotHasParent:
			nodes := captureNodes(pred.leftCapture, captures)
			if len(nodes) == 0 {
				return false
			}
			for _, n := range nodes {
				parent := n.Parent()
				if parent != nil && typeNameMatchesAny(parent.Type(lang), pred.values) {
					return false
				}
			}

		case predicateIs:
			if !predicateIsSatisfied(pred, captures) {
				return false
			}

		case predicateIsNot:
			if predicateIsSatisfied(pred, captures) {
				return false
			}

		case predicateSet, predicateOffset, predicateSelectAdjacent, predicateStrip:
			// Directives do not affect whether a match exists.
			continue

		default:
			return false
		}
	}

	return true
}

// applyDirectives applies capture-modifying directives (#select-adjacent!,
// #strip!) to the captures list after a match has been accepted.
func (q *Query) applyDirectives(predicates []QueryPredicate, captures []QueryCapture, source []byte) []QueryCapture {
	for _, pred := range predicates {
		switch pred.kind {
		case predicateSelectAdjacent:
			captures = applySelectAdjacent(pred, captures)
		case predicateStrip:
			captures = applyStrip(pred, captures, source)
		}
	}
	return captures
}

// applySelectAdjacent filters the captures named by pred.leftCapture to only
// those that are byte-adjacent to at least one capture named by
// pred.rightCapture. "Adjacent" means one node's end byte equals the other's
// start byte.
func applySelectAdjacent(pred QueryPredicate, captures []QueryCapture) []QueryCapture {
	itemsName := pred.leftCapture
	anchorName := pred.rightCapture

	// Collect anchor byte boundaries.
	type boundary struct {
		start, end uint32
	}
	var anchors []boundary
	for _, c := range captures {
		if c.Name == anchorName && c.Node != nil {
			anchors = append(anchors, boundary{c.Node.StartByte(), c.Node.EndByte()})
		}
	}
	if len(anchors) == 0 {
		// No anchors — remove all items captures.
		// Reuse the input backing array because captures is an ephemeral
		// per-match slice owned by directive application.
		out := captures[:0]
		for _, c := range captures {
			if c.Name != itemsName {
				out = append(out, c)
			}
		}
		return out
	}

	isAdjacent := func(n *Node) bool {
		if n == nil {
			return false
		}
		nStart := n.StartByte()
		nEnd := n.EndByte()
		for _, a := range anchors {
			if nEnd == a.start || nStart == a.end {
				return true
			}
		}
		return false
	}

	out := make([]QueryCapture, 0, len(captures))
	for _, c := range captures {
		if c.Name == itemsName {
			if isAdjacent(c.Node) {
				out = append(out, c)
			}
			continue
		}
		out = append(out, c)
	}
	return out
}

// applyStrip applies the #strip! directive: for each capture named by
// pred.leftCapture, it sets TextOverride to the node's text with all
// matches of pred.regex removed.
func applyStrip(pred QueryPredicate, captures []QueryCapture, source []byte) []QueryCapture {
	if pred.regex == nil {
		return captures
	}
	// Mutate captures in place: directive application owns this slice and the
	// updated TextOverride should be visible to downstream consumers.
	for i := range captures {
		if captures[i].Name == pred.leftCapture && captures[i].Node != nil {
			text := captures[i].Node.Text(source)
			stripped := pred.regex.ReplaceAllString(text, "")
			if stripped != text {
				captures[i].TextOverride = stripped
			}
		}
	}
	return captures
}

func captureNodes(name string, captures []QueryCapture) []*Node {
	var nodes []*Node
	for _, c := range captures {
		if c.Name == name && c.Node != nil {
			nodes = append(nodes, c.Node)
		}
	}
	return nodes
}

func typeNameMatchesAny(typeName string, names []string) bool {
	for _, n := range names {
		if n == typeName {
			return true
		}
	}
	return false
}

func nodeHasAncestorType(node *Node, typeNames []string, lang *Language) bool {
	if node == nil || lang == nil {
		return false
	}
	for p := node.Parent(); p != nil; p = p.Parent() {
		if typeNameMatchesAny(p.Type(lang), typeNames) {
			return true
		}
	}
	return false
}

func capturePropertyMatches(captureName string, property string) bool {
	prop := strings.Trim(property, "\"")
	switch prop {
	case "local":
		return strings.Contains(captureName, "local") || strings.Contains(captureName, "parameter")
	case "local.parameter", "parameter":
		return strings.Contains(captureName, "parameter")
	case "function":
		return strings.Contains(captureName, "function")
	case "var", "variable":
		return strings.Contains(captureName, "var") || strings.Contains(captureName, "variable")
	}
	if captureName == prop {
		return true
	}
	return strings.HasSuffix(captureName, "."+prop)
}

func predicateIsSatisfied(pred QueryPredicate, captures []QueryCapture) bool {
	if pred.property == "" {
		return false
	}
	if pred.leftCapture != "" {
		nodes := captureNodes(pred.leftCapture, captures)
		if len(nodes) == 0 {
			return false
		}
		return capturePropertyMatches(pred.leftCapture, pred.property)
	}

	for _, c := range captures {
		if capturePropertyMatches(c.Name, pred.property) {
			return true
		}
	}
	return false
}

func captureText(name string, captures []QueryCapture, source []byte) (string, bool) {
	for _, c := range captures {
		if c.Name == name {
			if source == nil {
				return "", false
			}
			return c.Node.Text(source), true
		}
	}
	return "", false
}

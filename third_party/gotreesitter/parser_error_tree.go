package gotreesitter

import "unicode/utf8"

func parseErrorTree(source []byte, lang *Language) *Tree {
	end := Point{}
	for i := 0; i < len(source); {
		if source[i] == '\n' {
			end.Row++
			end.Column = 0
			i++
			continue
		}
		_, size := utf8.DecodeRune(source[i:])
		if size <= 0 {
			size = 1
		}
		i += size
		end.Column++
	}

	root := NewLeafNode(errorSymbol, true, 0, uint32(len(source)), Point{}, end)
	root.hasError = true
	return NewTree(root, source, lang)
}

func isWhitespaceOnlySource(source []byte) bool {
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case ' ', '\t', '\n', '\r', '\f':
		default:
			return false
		}
	}
	return true
}

func extendNodeToTrailingWhitespace(n *Node, source []byte) {
	if n == nil {
		return
	}
	sourceEnd := uint32(len(source))
	if n.endByte >= sourceEnd {
		return
	}
	tail := source[n.endByte:sourceEnd]
	for i := 0; i < len(tail); i++ {
		switch tail[i] {
		case ' ', '\t', '\n', '\r', '\f':
		default:
			return
		}
	}

	pt := n.endPoint
	for i := 0; i < len(tail); {
		if tail[i] == '\n' {
			pt.Row++
			pt.Column = 0
			i++
			continue
		}
		_, size := utf8.DecodeRune(tail[i:])
		if size <= 0 {
			size = 1
		}
		i += size
		pt.Column++
	}

	n.endByte = sourceEnd
	n.endPoint = pt
}

package gotreesitter

import "sort"

func buildSmallLookup(lang *Language) [][]smallActionPair {
	out := make([][]smallActionPair, len(lang.SmallParseTableMap))
	table := lang.SmallParseTable
	for smallIdx, offset := range lang.SmallParseTableMap {
		pos := int(offset)
		if pos >= len(table) {
			continue
		}
		groupCount := table[pos]
		pos++
		total := 0
		countPos := pos
		for i := uint16(0); i < groupCount; i++ {
			if countPos+1 >= len(table) {
				total = 0
				break
			}
			symbolCount := int(table[countPos+1])
			total += symbolCount
			countPos += 2 + symbolCount
		}
		if total == 0 {
			continue
		}

		pairs := make([]smallActionPair, 0, total)
		for i := uint16(0); i < groupCount; i++ {
			if pos+1 >= len(table) {
				break
			}
			val := table[pos]
			symbolCount := table[pos+1]
			pos += 2
			for j := uint16(0); j < symbolCount; j++ {
				if pos >= len(table) {
					break
				}
				pairs = append(pairs, smallActionPair{sym: table[pos], val: val})
				pos++
			}
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].sym < pairs[j].sym })
		out[smallIdx] = pairs
	}
	return out
}

// lookupAction looks up the parse action for the given state and symbol.
func (p *Parser) lookupAction(state StateID, sym Symbol) *ParseActionEntry {
	idx := p.lookupActionIndex(state, sym)
	if idx == 0 {
		return nil
	}
	if int(idx) < len(p.language.ParseActions) {
		return &p.language.ParseActions[idx]
	}
	return nil
}

// lookupActionIndex returns the parse action index for (state, symbol).
// Returns 0 (the error/no-action entry) if not found.
func (p *Parser) lookupActionIndex(state StateID, sym Symbol) uint16 {
	if int(state) < p.denseLimit {
		return p.lookupActionIndexDense(state, sym)
	}
	return p.lookupActionIndexSmall(state, sym)
}

func (p *Parser) forEachActionIndexInState(state StateID, visit func(sym Symbol, idx uint16) bool) {
	if p == nil || p.language == nil || visit == nil {
		return
	}
	if int(state) < p.denseLimit {
		if int(state) >= len(p.language.ParseTable) {
			return
		}
		row := p.language.ParseTable[state]
		for sym, idx := range row {
			if idx == 0 {
				continue
			}
			if !visit(Symbol(sym), idx) {
				return
			}
		}
		return
	}

	smallIdx := int(state) - p.smallBase
	if smallIdx < 0 || smallIdx >= len(p.language.SmallParseTableMap) {
		return
	}
	if smallIdx < len(p.smallLookup) && len(p.smallLookup[smallIdx]) > 0 {
		for _, pair := range p.smallLookup[smallIdx] {
			if !visit(Symbol(pair.sym), pair.val) {
				return
			}
		}
		return
	}

	offset := p.language.SmallParseTableMap[smallIdx]
	table := p.language.SmallParseTable
	if int(offset) >= len(table) {
		return
	}
	groupCount := table[offset]
	pos := int(offset) + 1
	for i := uint16(0); i < groupCount; i++ {
		if pos+1 >= len(table) {
			return
		}
		sectionValue := table[pos]
		symbolCount := table[pos+1]
		pos += 2
		for j := uint16(0); j < symbolCount; j++ {
			if pos >= len(table) {
				return
			}
			if !visit(Symbol(table[pos]), sectionValue) {
				return
			}
			pos++
		}
	}
}

func (p *Parser) lookupActionIndexDense(state StateID, sym Symbol) uint16 {
	if int(state) >= len(p.language.ParseTable) {
		return 0
	}
	row := p.language.ParseTable[state]
	if int(sym) >= len(row) {
		return 0
	}
	return row[sym]
}

func (p *Parser) lookupActionIndexSmall(state StateID, sym Symbol) uint16 {
	// Small (compressed sparse) table lookup.
	smallIdx := int(state) - p.smallBase
	if smallIdx < 0 || smallIdx >= len(p.language.SmallParseTableMap) {
		return 0
	}
	if smallIdx < len(p.smallLookup) {
		pairs := p.smallLookup[smallIdx]
		if len(pairs) > 0 {
			target := uint16(sym)
			if len(pairs) <= 8 {
				for i := 0; i < len(pairs); i++ {
					if pairs[i].sym == target {
						return pairs[i].val
					}
					if pairs[i].sym > target {
						return 0
					}
				}
				return 0
			}
			lo, hi := 0, len(pairs)
			for lo < hi {
				mid := int(uint(lo+hi) >> 1)
				if pairs[mid].sym < target {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			if lo < len(pairs) && pairs[lo].sym == target {
				return pairs[lo].val
			}
			return 0
		}
	}
	offset := p.language.SmallParseTableMap[smallIdx]
	table := p.language.SmallParseTable
	if int(offset) >= len(table) {
		return 0
	}

	groupCount := table[offset]
	pos := int(offset) + 1
	for i := uint16(0); i < groupCount; i++ {
		if pos+1 >= len(table) {
			break
		}
		sectionValue := table[pos]
		symbolCount := table[pos+1]
		pos += 2
		for j := uint16(0); j < symbolCount; j++ {
			if pos >= len(table) {
				break
			}
			if table[pos] == uint16(sym) {
				return sectionValue
			}
			pos++
		}
	}
	return 0
}

// lookupGoto returns the GOTO target state for a nonterminal symbol.
func (p *Parser) lookupGoto(state StateID, sym Symbol) StateID {
	raw := p.lookupActionIndex(state, sym)
	if raw == 0 {
		return 0
	}

	// ts2go-generated grammars encode nonterminal GOTO values directly as
	// parser state IDs. Hand-built grammars encode parse-action indices.
	// ts2go always sets InitialState=1 (tree-sitter convention); hand-built
	// grammars default to InitialState=0.
	if p.language.TokenCount > 0 &&
		uint32(sym) >= p.language.TokenCount &&
		p.language.StateCount > 0 &&
		p.language.InitialState > 0 {
		return StateID(raw)
	}

	// Hand-built grammar or terminal symbol: look up in parse actions.
	if int(raw) < len(p.language.ParseActions) {
		entry := &p.language.ParseActions[raw]
		if len(entry.Actions) > 0 && entry.Actions[0].Type == ParseActionShift {
			return entry.Actions[0].State
		}
	}
	return 0
}

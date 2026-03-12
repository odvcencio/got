package gotreesitter

import "fmt"

// LookaheadIterator iterates over valid symbols for a given parse state.
// It precomputes the full set of symbols that have valid parse actions in
// the specified state, enabling autocomplete and error diagnostic use cases.
type LookaheadIterator struct {
	language *Language
	state    StateID
	symbols  []Symbol // precomputed valid symbols for this state
	pos      int      // current position, -1 before first Next()
}

// NewLookaheadIterator creates an iterator over all symbols that have valid
// parse actions in the given state. Returns an error if the state is out of
// range for the language's parse tables.
func NewLookaheadIterator(lang *Language, state StateID) (*LookaheadIterator, error) {
	if lang == nil {
		return nil, fmt.Errorf("lookahead: nil language")
	}
	it := &LookaheadIterator{
		language: lang,
		pos:      -1,
	}
	if err := it.ResetState(state); err != nil {
		return nil, err
	}
	return it, nil
}

// Next advances the iterator to the next valid symbol. Returns false when
// there are no more symbols.
func (it *LookaheadIterator) Next() bool {
	it.pos++
	return it.pos < len(it.symbols)
}

// CurrentSymbol returns the symbol at the current iterator position.
// Must be called after a successful Next().
func (it *LookaheadIterator) CurrentSymbol() Symbol {
	if it.pos < 0 || it.pos >= len(it.symbols) {
		return 0
	}
	return it.symbols[it.pos]
}

// CurrentSymbolName returns the name of the symbol at the current iterator
// position. Returns "" if the position is invalid or the symbol has no name.
func (it *LookaheadIterator) CurrentSymbolName() string {
	sym := it.CurrentSymbol()
	if int(sym) < len(it.language.SymbolNames) {
		return it.language.SymbolNames[sym]
	}
	return ""
}

// ResetState resets the iterator to enumerate valid symbols for a different
// parse state within the same language. Returns an error if the state is
// out of range.
func (it *LookaheadIterator) ResetState(state StateID) error {
	stateInt := int(state)
	maxState := computeMaxState(it.language)
	if stateInt >= maxState {
		return fmt.Errorf("lookahead: state %d out of range (max %d)", state, maxState-1)
	}

	it.state = state
	it.pos = -1
	it.symbols = collectValidSymbols(it.language, state)
	return nil
}

// Language returns the language associated with this iterator.
func (it *LookaheadIterator) Language() *Language {
	return it.language
}

// computeMaxState returns the total number of parser states for the language.
func computeMaxState(lang *Language) int {
	max := int(lang.StateCount)
	if max <= 0 {
		max = len(lang.ParseTable)
	}
	if smallCount := int(lang.LargeStateCount) + len(lang.SmallParseTableMap); smallCount > max {
		max = smallCount
	}
	return max
}

// collectValidSymbols returns all symbols that have a non-zero action index
// in the given parse state.
func collectValidSymbols(lang *Language, state StateID) []Symbol {
	s := int(state)

	// Dense table: state < LargeStateCount (and within ParseTable bounds).
	if s < len(lang.ParseTable) && s < int(lang.LargeStateCount) {
		row := lang.ParseTable[s]
		symbols := make([]Symbol, 0, 8)
		for sym, actionIdx := range row {
			if actionIdx != 0 {
				symbols = append(symbols, Symbol(sym))
			}
		}
		return symbols
	}

	// Small/sparse table.
	smallIdx := s - int(lang.LargeStateCount)
	if smallIdx < 0 || smallIdx >= len(lang.SmallParseTableMap) {
		return nil
	}
	if len(lang.SmallParseTable) == 0 {
		return nil
	}

	table := lang.SmallParseTable
	offset := lang.SmallParseTableMap[smallIdx]
	pos := int(offset)
	if pos >= len(table) {
		return nil
	}

	groupCount := table[pos]
	pos++

	symbols := make([]Symbol, 0, 8)
	for i := uint16(0); i < groupCount; i++ {
		if pos+1 >= len(table) {
			break
		}
		sectionValue := table[pos]
		symbolCount := int(table[pos+1])
		pos += 2
		// Only include symbols that map to a non-zero action index.
		if sectionValue != 0 {
			for j := 0; j < symbolCount; j++ {
				if pos >= len(table) {
					break
				}
				symbols = append(symbols, Symbol(table[pos]))
				pos++
			}
		} else {
			// Skip past symbols with zero action index.
			pos += symbolCount
			if pos > len(table) {
				pos = len(table)
			}
		}
	}
	return symbols
}

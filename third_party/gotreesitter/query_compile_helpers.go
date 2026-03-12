package gotreesitter

import (
	"fmt"
	"strings"
	"unicode"
)

// readIdentifier reads an identifier (node type name, field name).
// Identifiers can contain letters, digits, underscores, dots, and hyphens.

func (p *queryParser) readStepQuantifier() (queryQuantifier, bool) {
	if p.pos >= len(p.input) {
		return queryQuantifierOne, false
	}
	switch p.input[p.pos] {
	case '?':
		p.pos++
		return queryQuantifierZeroOrOne, true
	case '*':
		p.pos++
		return queryQuantifierZeroOrMore, true
	case '+':
		p.pos++
		return queryQuantifierOneOrMore, true
	default:
		return queryQuantifierOne, false
	}
}

func (p *queryParser) readIdentifier() (string, error) {
	start := p.pos
	for p.pos < len(p.input) {
		ch := rune(p.input[p.pos])
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '.' || ch == '-' || ch == '/' {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return "", fmt.Errorf("query: expected identifier at position %d", p.pos)
	}
	return p.input[start:p.pos], nil
}

// readCapture reads a @capture_name token. It consumes the '@' and the name.
func (p *queryParser) readCapture() (string, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '@' {
		return "", fmt.Errorf("query: expected '@' at position %d", p.pos)
	}
	p.pos++ // consume '@'
	name, err := p.readIdentifier()
	if err != nil {
		return "", fmt.Errorf("query: expected capture name after '@': %w", err)
	}
	return name, nil
}

// readString reads a quoted string like "func". Consumes the quotes.
func (p *queryParser) readString() (string, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '"' {
		return "", fmt.Errorf("query: expected '\"' at position %d", p.pos)
	}
	p.pos++ // consume opening '"'
	var sb strings.Builder
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == '\\' && p.pos+1 < len(p.input) {
			p.pos++
			sb.WriteByte(p.input[p.pos])
			p.pos++
			continue
		}
		if ch == '"' {
			p.pos++ // consume closing '"'
			out := sb.String()
			p.ensureString(out)
			return out, nil
		}
		sb.WriteByte(ch)
		p.pos++
	}
	return "", fmt.Errorf("query: unterminated string")
}

// skipWhitespaceAndComments skips whitespace and ;-style line comments.
func (p *queryParser) skipWhitespaceAndComments() {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			p.pos++
			continue
		}
		if ch == ';' {
			// Skip to end of line.
			for p.pos < len(p.input) && p.input[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}

// resolveSymbol looks up a node type name in the language, returning the
// symbol ID and whether it's a named symbol. Uses Language.SymbolByName
// for O(1) lookup.
func (p *queryParser) resolveSymbol(name string) (Symbol, bool, error) {
	if name == "_" {
		return 0, false, nil
	}
	if name == "ERROR" {
		return errorSymbol, true, nil
	}
	if name == "MISSING" {
		return 0, false, nil
	}

	sym, ok := p.lang.SymbolByName(name)
	if !ok {
		// Some highlight queries use supertype-like names such as
		// "pattern/wildcard". Fall back to the rightmost segment when needed.
		if idx := strings.LastIndex(name, "/"); idx >= 0 && idx+1 < len(name) {
			if fallback, fallbackOK := p.lang.SymbolByName(name[idx+1:]); fallbackOK {
				sym = fallback
				ok = true
			}
		}
	}
	if !ok {
		return 0, false, queryUnknownNodeTypeError{name: name}
	}
	isNamed := false
	if int(sym) < len(p.lang.SymbolMetadata) {
		isNamed = p.lang.SymbolMetadata[sym].Named
	}
	return sym, isNamed, nil
}

// resolveField looks up a field name in the language with compatibility
// fallbacks for grammar/query naming drift.
func (p *queryParser) resolveField(name string, parentSymbol Symbol, parentSymbolHint Symbol) (FieldID, error) {
	fid, ok := p.lang.FieldByName(name)
	if ok {
		return fid, nil
	}

	// Some bundled queries use short field names like "key" while grammars
	// expose prefixed names (for example "option_key"). If parent type is
	// known, try parentName_fieldName first.
	seenSymbols := map[Symbol]struct{}{}
	for _, sym := range []Symbol{parentSymbol, parentSymbolHint} {
		if _, seen := seenSymbols[sym]; seen {
			continue
		}
		seenSymbols[sym] = struct{}{}
		if int(sym) < 0 || int(sym) >= len(p.lang.SymbolNames) {
			continue
		}
		parentName := p.lang.SymbolNames[sym]
		if parentName == "" {
			continue
		}
		candidate := parentName + "_" + name
		if fid, ok := p.lang.FieldByName(candidate); ok {
			return fid, nil
		}
		// Allow nested names like "foo/bar" -> "bar_name".
		if idx := strings.LastIndex(parentName, "/"); idx >= 0 && idx+1 < len(parentName) {
			candidate = parentName[idx+1:] + "_" + name
			if fid, ok := p.lang.FieldByName(candidate); ok {
				return fid, nil
			}
		}
	}

	// As a final fallback, allow a unique *_name suffix match.
	matchID := FieldID(0)
	matchCount := 0
	suffix := "_" + name
	for id, fieldName := range p.lang.FieldNames {
		if fieldName == "" {
			continue
		}
		if strings.HasSuffix(fieldName, suffix) {
			matchID = FieldID(id)
			matchCount++
		}
	}
	if matchCount == 1 {
		return matchID, nil
	}

	return 0, fmt.Errorf("query: unknown field name %q", name)
}

// ensureCapture returns the index for a capture name, adding it if new.
func (p *queryParser) ensureCapture(name string) int {
	for i, cn := range p.q.captures {
		if cn == name {
			return i
		}
	}
	idx := len(p.q.captures)
	p.q.captures = append(p.q.captures, name)
	return idx
}

// ensureString returns the index for a string literal, adding it if new.
func (p *queryParser) ensureString(value string) int {
	for i, s := range p.q.strings {
		if s == value {
			return i
		}
	}
	idx := len(p.q.strings)
	p.q.strings = append(p.q.strings, value)
	return idx
}

func (p *queryParser) addCaptureToStep(step *QueryStep, captureID int) {
	if step.captureID < 0 {
		step.captureID = captureID
	}
	step.captureIDs = append(step.captureIDs, captureID)
}

func (p *queryParser) addCaptureToAlternative(alt *alternativeSymbol, captureID int) {
	if alt.captureID < 0 {
		alt.captureID = captureID
	}
	alt.captureIDs = append(alt.captureIDs, captureID)
}

// peekNextIsPatternElement checks whether the next non-whitespace token
// starts a new pattern element (child or sibling pattern), as opposed to
// a capture (@), predicate (#), close paren, or negation (!).
func (p *queryParser) peekNextIsPatternElement() bool {
	saved := p.pos
	defer func() { p.pos = saved }()
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) {
		return false
	}
	ch := p.input[p.pos]
	switch {
	case ch == '(':
		return p.pos+1 < len(p.input) && p.input[p.pos+1] != '#'
	case ch == '[', ch == '"', ch == '.':
		return true
	case isIdentStart(ch):
		return true
	default:
		return false
	}
}

// isIdentStart reports whether a byte can start an identifier.
func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

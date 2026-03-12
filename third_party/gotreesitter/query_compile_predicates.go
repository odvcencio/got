package gotreesitter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func (p *queryParser) parsePredicate() (QueryPredicate, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return QueryPredicate{}, fmt.Errorf("query: expected '(' at position %d", p.pos)
	}
	p.pos++ // consume '('
	p.skipWhitespaceAndComments()

	name, err := p.readPredicateName()
	if err != nil {
		return QueryPredicate{}, err
	}

	switch name {
	case "#eq?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		pred := QueryPredicate{
			kind:        predicateEq,
			leftCapture: left,
		}
		if rightIsCapture {
			pred.rightCapture = right
		} else {
			pred.literal = right
		}
		return pred, nil

	case "#not-eq?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		pred := QueryPredicate{
			kind:        predicateNotEq,
			leftCapture: left,
		}
		if rightIsCapture {
			pred.rightCapture = right
		} else {
			pred.literal = right
		}
		return pred, nil

	case "#match?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		if rightIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #match? second argument must be a string literal")
		}
		rx, err := regexp.Compile(right)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid regex in #match?: %w", err)
		}
		return QueryPredicate{
			kind:        predicateMatch,
			leftCapture: left,
			literal:     right,
			regex:       rx,
		}, nil

	case "#not-match?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		if rightIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #not-match? second argument must be a string literal")
		}
		rx, err := regexp.Compile(right)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid regex in #not-match?: %w", err)
		}
		return QueryPredicate{
			kind:        predicateNotMatch,
			leftCapture: left,
			literal:     right,
			regex:       rx,
		}, nil

	case "#any-eq?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		pred := QueryPredicate{
			kind:        predicateAnyEq,
			leftCapture: left,
		}
		if rightIsCapture {
			pred.rightCapture = right
		} else {
			pred.literal = right
		}
		return pred, nil

	case "#any-not-eq?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		pred := QueryPredicate{
			kind:        predicateAnyNotEq,
			leftCapture: left,
		}
		if rightIsCapture {
			pred.rightCapture = right
		} else {
			pred.literal = right
		}
		return pred, nil

	case "#any-match?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		if rightIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #any-match? second argument must be a string literal")
		}
		rx, err := regexp.Compile(right)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid regex in #any-match?: %w", err)
		}
		return QueryPredicate{
			kind:        predicateAnyMatch,
			leftCapture: left,
			literal:     right,
			regex:       rx,
		}, nil

	case "#any-not-match?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		if rightIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #any-not-match? second argument must be a string literal")
		}
		rx, err := regexp.Compile(right)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid regex in #any-not-match?: %w", err)
		}
		return QueryPredicate{
			kind:        predicateAnyNotMatch,
			leftCapture: left,
			literal:     right,
			regex:       rx,
		}, nil

	case "#lua-match?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		p.skipWhitespaceAndComments()
		right, rightIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		if rightIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #lua-match? second argument must be a string literal")
		}
		rx, err := compileLuaPattern(right)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid lua pattern in #lua-match?: %w", err)
		}
		return QueryPredicate{
			kind:        predicateLuaMatch,
			leftCapture: left,
			literal:     right,
			regex:       rx,
		}, nil

	case "#any-of?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		var values []string
		for {
			p.skipWhitespaceAndComments()
			if p.pos >= len(p.input) {
				return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
			}
			if p.input[p.pos] == ')' {
				p.pos++ // consume ')'
				break
			}
			v, kind, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			if kind == predicateArgCapture {
				return QueryPredicate{}, fmt.Errorf("query: #any-of? arguments after first must be non-capture literals")
			}
			values = append(values, v)
		}
		if len(values) == 0 {
			return QueryPredicate{}, fmt.Errorf("query: #any-of? requires at least one string literal")
		}
		return QueryPredicate{
			kind:        predicateAnyOf,
			leftCapture: left,
			values:      values,
		}, nil

	case "#not-any-of?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		var values []string
		for {
			p.skipWhitespaceAndComments()
			if p.pos >= len(p.input) {
				return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
			}
			if p.input[p.pos] == ')' {
				p.pos++ // consume ')'
				break
			}
			v, kind, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			if kind == predicateArgCapture {
				return QueryPredicate{}, fmt.Errorf("query: #not-any-of? arguments after first must be non-capture literals")
			}
			values = append(values, v)
		}
		if len(values) == 0 {
			return QueryPredicate{}, fmt.Errorf("query: #not-any-of? requires at least one literal")
		}
		return QueryPredicate{
			kind:        predicateNotAnyOf,
			leftCapture: left,
			values:      values,
		}, nil

	case "#has-ancestor?", "#not-has-ancestor?", "#not-has-parent?":
		p.skipWhitespaceAndComments()
		left, leftIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !leftIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: first predicate argument must be a capture in %s", name)
		}

		var types []string
		for {
			p.skipWhitespaceAndComments()
			if p.pos >= len(p.input) {
				return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
			}
			if p.input[p.pos] == ')' {
				p.pos++ // consume ')'
				break
			}
			tok, kind, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			if kind == predicateArgCapture {
				return QueryPredicate{}, fmt.Errorf("query: %s node type arguments must be non-capture identifiers", name)
			}
			types = append(types, tok)
		}
		if len(types) == 0 {
			return QueryPredicate{}, fmt.Errorf("query: %s requires at least one node type name", name)
		}
		kind := predicateHasAncestor
		if name == "#not-has-ancestor?" {
			kind = predicateNotHasAncestor
		}
		if name == "#not-has-parent?" {
			kind = predicateNotHasParent
		}
		return QueryPredicate{
			kind:        kind,
			leftCapture: left,
			values:      types,
		}, nil

	case "#is?", "#is-not?":
		var args []struct {
			value string
			kind  predicateArgKind
		}
		for {
			p.skipWhitespaceAndComments()
			if p.pos >= len(p.input) {
				return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
			}
			if p.input[p.pos] == ')' {
				p.pos++ // consume ')'
				break
			}
			tok, kind, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			args = append(args, struct {
				value string
				kind  predicateArgKind
			}{value: tok, kind: kind})
		}
		if len(args) == 0 {
			return QueryPredicate{}, fmt.Errorf("query: %s requires arguments", name)
		}

		pred := QueryPredicate{}
		if name == "#is?" {
			pred.kind = predicateIs
		} else {
			pred.kind = predicateIsNot
		}

		if args[0].kind == predicateArgCapture {
			pred.leftCapture = args[0].value
			if len(args) < 2 {
				return QueryPredicate{}, fmt.Errorf("query: %s capture form requires a property argument", name)
			}
			if args[1].kind == predicateArgCapture {
				return QueryPredicate{}, fmt.Errorf("query: %s property argument cannot be a capture", name)
			}
			pred.property = args[1].value
		} else {
			pred.property = args[0].value
			if len(args) >= 2 {
				if args[1].kind != predicateArgCapture {
					return QueryPredicate{}, fmt.Errorf("query: %s second argument must be a capture when provided", name)
				}
				pred.leftCapture = args[1].value
			}
		}
		return pred, nil

	case "#set!":
		p.skipWhitespaceAndComments()
		key, kind, err := p.readPredicateToken()
		if err != nil {
			return QueryPredicate{}, err
		}
		if kind == predicateArgCapture {
			return QueryPredicate{}, fmt.Errorf("query: #set! key must be a non-capture token")
		}
		var values []string
		for {
			p.skipWhitespaceAndComments()
			if p.pos >= len(p.input) {
				return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
			}
			if p.input[p.pos] == ')' {
				p.pos++
				break
			}
			v, _, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			values = append(values, v)
		}
		return QueryPredicate{
			kind:    predicateSet,
			literal: key,
			values:  values,
		}, nil

	case "#offset!":
		p.skipWhitespaceAndComments()
		capName, kind, err := p.readPredicateToken()
		if err != nil {
			return QueryPredicate{}, err
		}
		if kind != predicateArgCapture {
			return QueryPredicate{}, fmt.Errorf("query: #offset! first argument must be a capture")
		}
		var nums [4]int
		for i := 0; i < 4; i++ {
			p.skipWhitespaceAndComments()
			tok, tokKind, err := p.readPredicateToken()
			if err != nil {
				return QueryPredicate{}, err
			}
			if tokKind == predicateArgCapture {
				return QueryPredicate{}, fmt.Errorf("query: #offset! numeric arguments must be literals")
			}
			n, convErr := strconv.Atoi(tok)
			if convErr != nil {
				return QueryPredicate{}, fmt.Errorf("query: #offset! invalid integer %q", tok)
			}
			nums[i] = n
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'
		return QueryPredicate{
			kind:        predicateOffset,
			leftCapture: capName,
			offset:      nums,
		}, nil

	case "#select-adjacent!":
		p.skipWhitespaceAndComments()
		items, itemsIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !itemsIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #select-adjacent! first argument must be a capture")
		}

		p.skipWhitespaceAndComments()
		anchor, anchorIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !anchorIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #select-adjacent! second argument must be a capture")
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		return QueryPredicate{
			kind:         predicateSelectAdjacent,
			leftCapture:  items,
			rightCapture: anchor,
		}, nil

	case "#strip!":
		p.skipWhitespaceAndComments()
		capName, capIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if !capIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #strip! first argument must be a capture")
		}

		p.skipWhitespaceAndComments()
		pattern, patternIsCapture, err := p.readPredicateArg()
		if err != nil {
			return QueryPredicate{}, err
		}
		if patternIsCapture {
			return QueryPredicate{}, fmt.Errorf("query: #strip! second argument must be a string literal (regex)")
		}
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return QueryPredicate{}, fmt.Errorf("query: expected ')' to close predicate at position %d", p.pos)
		}
		p.pos++ // consume ')'

		rx, err := regexp.Compile(pattern)
		if err != nil {
			return QueryPredicate{}, fmt.Errorf("query: invalid regex in #strip!: %w", err)
		}
		return QueryPredicate{
			kind:        predicateStrip,
			leftCapture: capName,
			literal:     pattern,
			regex:       rx,
		}, nil

	default:
		return QueryPredicate{}, fmt.Errorf("query: unsupported predicate %q", name)
	}
}

func compileLuaPattern(pattern string) (*regexp.Regexp, error) {
	var out strings.Builder
	inClass := false

	writeLuaClass := func(ch byte, inClass bool) bool {
		if inClass {
			switch ch {
			case 'a':
				out.WriteString("A-Za-z")
			case 'c':
				out.WriteString("[:cntrl:]")
			case 'd':
				out.WriteString("0-9")
			case 'l':
				out.WriteString("a-z")
			case 'p':
				out.WriteString("[:punct:]")
			case 's':
				out.WriteString("\\s")
			case 'u':
				out.WriteString("A-Z")
			case 'w':
				out.WriteString("A-Za-z0-9")
			case 'x':
				out.WriteString("A-Fa-f0-9")
			case 'z':
				out.WriteString("\\x00")
			default:
				return false
			}
			return true
		}
		switch ch {
		case 'a':
			out.WriteString("[A-Za-z]")
		case 'c':
			out.WriteString("[[:cntrl:]]")
		case 'd':
			out.WriteString("[0-9]")
		case 'l':
			out.WriteString("[a-z]")
		case 'p':
			out.WriteString("[[:punct:]]")
		case 's':
			out.WriteString("\\s")
		case 'u':
			out.WriteString("[A-Z]")
		case 'w':
			out.WriteString("[A-Za-z0-9]")
		case 'x':
			out.WriteString("[A-Fa-f0-9]")
		case 'z':
			out.WriteString("\\x00")
		default:
			return false
		}
		return true
	}

	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '[':
			inClass = true
			out.WriteByte(ch)
		case ']':
			inClass = false
			out.WriteByte(ch)
		case '%':
			if i+1 >= len(pattern) {
				out.WriteString("%")
				continue
			}
			i++
			next := pattern[i]
			if writeLuaClass(next, inClass) {
				continue
			}
			out.WriteString(regexp.QuoteMeta(string(next)))
		default:
			out.WriteByte(ch)
		}
	}

	return regexp.Compile(out.String())
}

func (p *queryParser) readPredicateName() (string, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '#' {
		return "", fmt.Errorf("query: expected predicate name at position %d", p.pos)
	}
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ')' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("query: expected predicate name at position %d", start)
	}
	return p.input[start:p.pos], nil
}

type predicateArgKind uint8

const (
	predicateArgCapture predicateArgKind = iota
	predicateArgString
	predicateArgAtom
)

func (p *queryParser) readPredicateToken() (arg string, kind predicateArgKind, err error) {
	if p.pos >= len(p.input) {
		return "", predicateArgAtom, fmt.Errorf("query: expected predicate argument at end of input")
	}

	switch p.input[p.pos] {
	case '@':
		name, err := p.readCapture()
		if err != nil {
			return "", predicateArgAtom, err
		}
		return name, predicateArgCapture, nil
	case '"':
		text, err := p.readString()
		if err != nil {
			return "", predicateArgAtom, err
		}
		return text, predicateArgString, nil
	default:
		start := p.pos
		for p.pos < len(p.input) {
			ch := p.input[p.pos]
			if ch == ')' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				break
			}
			p.pos++
		}
		if p.pos == start {
			return "", predicateArgAtom, fmt.Errorf("query: expected predicate argument at position %d", p.pos)
		}
		return p.input[start:p.pos], predicateArgAtom, nil
	}
}

func (p *queryParser) readPredicateArg() (arg string, isCapture bool, err error) {
	if p.pos >= len(p.input) {
		return "", false, fmt.Errorf("query: expected predicate argument at end of input")
	}

	switch p.input[p.pos] {
	case '@':
		name, err := p.readCapture()
		if err != nil {
			return "", false, err
		}
		return name, true, nil
	case '"':
		text, err := p.readString()
		if err != nil {
			return "", false, err
		}
		return text, false, nil
	default:
		return "", false, fmt.Errorf("query: expected capture or string literal in predicate at position %d", p.pos)
	}
}

package gotreesitter

import (
	"fmt"
)

type queryParser struct {
	input string
	pos   int
	lang  *Language
	q     *Query
}

func (p *queryParser) parse() error {
	for {
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) {
			break
		}

		ch := p.input[p.pos]

		if ch == '(' && p.pos+1 < len(p.input) && p.input[p.pos+1] == '#' {
			if len(p.q.patterns) == 0 {
				return fmt.Errorf("query: predicate must follow a pattern at position %d", p.pos)
			}
			pred, err := p.parsePredicate()
			if err != nil {
				return err
			}
			last := &p.q.patterns[len(p.q.patterns)-1]
			last.predicates = append(last.predicates, pred)
			last.endByte = uint32(p.pos)
			if err := p.validatePatternPredicates(last); err != nil {
				return err
			}
			continue
		}

		switch {
		case ch == '(':
			// A top-level pattern.
			startByte := uint32(p.pos)
			pat, err := p.parsePattern(0, 0)
			if err != nil {
				return err
			}
			pat.startByte = startByte
			pat.endByte = uint32(p.pos)
			p.q.patterns = append(p.q.patterns, *pat)

		case ch == '[':
			// Top-level alternation: ["func" "return"] @keyword
			startByte := uint32(p.pos)
			pat, err := p.parseAlternationPattern(0, 0)
			if err != nil {
				return err
			}
			pat.startByte = startByte
			pat.endByte = uint32(p.pos)
			p.q.patterns = append(p.q.patterns, *pat)

		case ch == '"':
			// Top-level string match: "func" @keyword
			startByte := uint32(p.pos)
			pat, err := p.parseStringPattern(0)
			if err != nil {
				return err
			}
			pat.startByte = startByte
			pat.endByte = uint32(p.pos)
			p.q.patterns = append(p.q.patterns, *pat)

		case isIdentStart(ch):
			// Top-level field shorthand: field: (pattern)
			startByte := uint32(p.pos)
			pat, err := p.parseFieldShorthandPattern(0)
			if err != nil {
				return err
			}
			pat.startByte = startByte
			pat.endByte = uint32(p.pos)
			p.q.patterns = append(p.q.patterns, *pat)

		case ch == '.':
			return fmt.Errorf("query: unexpected top-level anchor '.' at position %d", p.pos)

		default:
			return fmt.Errorf("query: unexpected character %q at position %d", string(ch), p.pos)
		}
	}
	return nil
}

// parsePattern parses a parenthesized S-expression pattern.
// depth is the nesting depth for the steps produced.
func (p *queryParser) parsePattern(depth int, parentSymbolHint Symbol) (*Pattern, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return nil, fmt.Errorf("query: expected '(' at position %d", p.pos)
	}
	p.pos++ // consume '('
	p.skipWhitespaceAndComments()

	pat := &Pattern{}
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("query: unexpected end of input, expected node type or pattern")
	}

	rootIdx := -1

	// Parse the root element. This supports:
	//   - standard node patterns: (identifier ...)
	//   - parenthesized strings: ("(") @punctuation.bracket
	//   - grouping wrappers: ((identifier) ... (#set! ...))
	switch ch := p.input[p.pos]; {
	case isIdentStart(ch):
		nodeType, err := p.readIdentifier()
		if err != nil {
			return nil, fmt.Errorf("query: expected node type after '(' at position %d: %w", p.pos, err)
		}
		step, err := p.stepFromIdentifierName(depth, nodeType)
		if err != nil {
			return nil, err
		}
		pat.steps = append(pat.steps, step)
		rootIdx = 0

	case ch == '"':
		text, err := p.readString()
		if err != nil {
			return nil, err
		}
		pat.steps = append(pat.steps, QueryStep{
			captureID: -1,
			depth:     depth,
			textMatch: text,
		})
		rootIdx = 0

	case ch == '(' || ch == '[':
		innerPat, err := p.parsePatternElement(depth, parentSymbolHint)
		if err != nil {
			return nil, err
		}
		if len(innerPat.steps) == 0 {
			return nil, fmt.Errorf("query: empty grouped pattern at position %d", p.pos)
		}

		if p.peekNextIsPatternElement() {
			// Multi-sibling group: ((a) (b) ...) — insert wildcard root.
			pat.steps = append(pat.steps, QueryStep{
				symbol:    0,
				isNamed:   false,
				captureID: -1,
				depth:     depth,
			})
			rootIdx = 0
			for i := range innerPat.steps {
				innerPat.steps[i].depth++
			}
			pat.steps = append(pat.steps, innerPat.steps...)
		} else {
			// Single-element group: ((a) @cap (#pred)) — unchanged.
			pat.steps = append(pat.steps, innerPat.steps...)
			rootIdx = 0
		}
		pat.predicates = append(pat.predicates, innerPat.predicates...)

	default:
		return nil, fmt.Errorf("query: expected node type after '(' at position %d: query: expected identifier at position %d", p.pos, p.pos)
	}

	// Parse children, fields, and captures until ')'.
	pendingAnchor := false
	lastChildRootIdx := -1
	appendChildPattern := func(childPat *Pattern) {
		if childPat == nil || len(childPat.steps) == 0 {
			return
		}
		if pendingAnchor {
			childPat.steps[0].anchorBefore = true
			pendingAnchor = false
		}
		childRootIdx := len(pat.steps)
		pat.predicates = append(pat.predicates, childPat.predicates...)
		pat.steps = append(pat.steps, childPat.steps...)
		lastChildRootIdx = childRootIdx
	}
	for {
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("query: unexpected end of input, expected ')'")
		}

		ch := p.input[p.pos]

		if ch == ')' {
			if pendingAnchor && lastChildRootIdx >= 0 {
				pat.steps[lastChildRootIdx].anchorAfter = true
			}
			p.pos++ // consume ')'
			break
		}

		if ch == '.' {
			// Anchor operators:
			//   - before child: first-child / immediate-sibling anchor
			//   - after child: last-child anchor
			// Anchors only affect child constraints at this depth.
			p.pos++
			pendingAnchor = true
			continue
		}

		if ch == '!' {
			// Field-negation constraint like !type_parameters.
			p.pos++
			p.skipWhitespaceAndComments()
			fieldName, err := p.readIdentifier()
			if err != nil {
				return nil, err
			}
			if rootIdx >= 0 && rootIdx < len(pat.steps) {
				parentSymbol := pat.steps[rootIdx].symbol
				fieldID, err := p.resolveField(fieldName, parentSymbol, parentSymbolHint)
				if err != nil {
					return nil, err
				}
				pat.steps[rootIdx].absentFields = append(pat.steps[rootIdx].absentFields, fieldID)
			}
			continue
		}

		if ch == '@' {
			// Capture for the current node.
			capName, err := p.readCapture()
			if err != nil {
				return nil, err
			}
			capID := p.ensureCapture(capName)
			if rootIdx >= 0 && rootIdx < len(pat.steps) {
				p.addCaptureToStep(&pat.steps[rootIdx], capID)
			}
			continue
		}

		if ch == '(' {
			// Predicate expression.
			if p.pos+1 < len(p.input) && p.input[p.pos+1] == '#' {
				pred, err := p.parsePredicate()
				if err != nil {
					return nil, err
				}
				pat.predicates = append(pat.predicates, pred)
				continue
			}

			// Nested pattern (child constraint).
			currentRootSymbol := Symbol(0)
			if rootIdx >= 0 && rootIdx < len(pat.steps) {
				currentRootSymbol = pat.steps[rootIdx].symbol
			}
			childPat, err := p.parsePatternElement(depth+1, currentRootSymbol)
			if err != nil {
				return nil, err
			}
			appendChildPattern(childPat)
			continue
		}

		if ch == '[' {
			// Alternation child.
			currentRootSymbol := Symbol(0)
			if rootIdx >= 0 && rootIdx < len(pat.steps) {
				currentRootSymbol = pat.steps[rootIdx].symbol
			}
			childPat, err := p.parsePatternElement(depth+1, currentRootSymbol)
			if err != nil {
				return nil, err
			}
			appendChildPattern(childPat)
			continue
		}

		if ch == '"' {
			// String child.
			currentRootSymbol := Symbol(0)
			if rootIdx >= 0 && rootIdx < len(pat.steps) {
				currentRootSymbol = pat.steps[rootIdx].symbol
			}
			childPat, err := p.parsePatternElement(depth+1, currentRootSymbol)
			if err != nil {
				return nil, err
			}
			appendChildPattern(childPat)
			continue
		}

		// Check for field: syntax (identifier followed by ':')
		if isIdentStart(ch) {
			ident, err := p.readIdentifier()
			if err != nil {
				return nil, err
			}
			afterIdent := p.pos
			p.skipWhitespaceAndComments()
			if p.pos < len(p.input) && p.input[p.pos] == ':' {
				// It's a field constraint.
				p.pos++ // consume ':'
				p.skipWhitespaceAndComments()

				parentSymbol := Symbol(0)
				if rootIdx >= 0 && rootIdx < len(pat.steps) {
					parentSymbol = pat.steps[rootIdx].symbol
				}
				fieldID, err := p.resolveField(ident, parentSymbol, parentSymbolHint)
				if err != nil {
					return nil, err
				}

				// The child pattern follows.
				if p.pos >= len(p.input) {
					return nil, fmt.Errorf("query: expected child pattern after field %q", ident)
				}

				childPat, err := p.parsePatternElement(depth+1, parentSymbol)
				if err != nil {
					return nil, err
				}
				if len(childPat.steps) > 0 {
					childPat.steps[0].field = fieldID
				}
				appendChildPattern(childPat)
			} else {
				// Bare shorthand child pattern like `_` or `identifier`.
				p.pos = afterIdent
				childPat, err := p.parseIdentifierPatternFromName(depth+1, ident)
				if err != nil {
					return nil, err
				}
				appendChildPattern(childPat)
			}
			continue
		}

		return nil, fmt.Errorf("query: unexpected character %q at position %d", string(ch), p.pos)
	}

	// Check for capture after the closing paren.
	p.skipWhitespaceAndComments()
	if quantifier, ok := p.readStepQuantifier(); ok {
		if rootIdx >= 0 && rootIdx < len(pat.steps) {
			pat.steps[rootIdx].quantifier = quantifier
		}
		p.skipWhitespaceAndComments()
	}
	for p.pos < len(p.input) && p.input[p.pos] == '@' {
		capName, err := p.readCapture()
		if err != nil {
			return nil, err
		}
		capID := p.ensureCapture(capName)
		if rootIdx >= 0 && rootIdx < len(pat.steps) {
			p.addCaptureToStep(&pat.steps[rootIdx], capID)
		}
		p.skipWhitespaceAndComments()
	}

	if err := p.validatePatternPredicates(pat); err != nil {
		return nil, err
	}

	return pat, nil
}

// parseAlternationPattern parses [...] alternation syntax.
func (p *queryParser) parseAlternationPattern(depth int, parentSymbolHint Symbol) (*Pattern, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '[' {
		return nil, fmt.Errorf("query: expected '[' at position %d", p.pos)
	}
	p.pos++ // consume '['
	p.skipWhitespaceAndComments()

	var alts []alternativeSymbol

	for {
		p.skipWhitespaceAndComments()
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("query: unexpected end of input in alternation")
		}

		if p.input[p.pos] == ']' {
			p.pos++ // consume ']'
			break
		}

		ch := p.input[p.pos]
		if ch == '.' {
			// Anchors inside alternations are parsed for compatibility and ignored.
			p.pos++
			continue
		}

		var branchPat *Pattern
		var err error
		altField := FieldID(0)
		if ch == '(' || ch == '[' || ch == '"' {
			branchPat, err = p.parsePatternElement(depth, parentSymbolHint)
		} else if isIdentStart(ch) {
			// Alternation may contain field shorthand branches like:
			// [name: (identifier) alias: (identifier)].
			ident, readErr := p.readIdentifier()
			if readErr != nil {
				return nil, readErr
			}
			p.skipWhitespaceAndComments()
			if p.pos < len(p.input) && p.input[p.pos] == ':' {
				p.pos++ // consume ':'
				p.skipWhitespaceAndComments()
				fieldID, fieldErr := p.resolveField(ident, parentSymbolHint, parentSymbolHint)
				if fieldErr != nil {
					return nil, fieldErr
				}
				altField = fieldID
				branchPat, err = p.parsePatternElement(depth, parentSymbolHint)
			} else {
				branchPat, err = p.parseIdentifierPatternFromName(depth, ident)
			}
		} else {
			return nil, fmt.Errorf("query: unexpected character %q in alternation at position %d", string(ch), p.pos)
		}
		if err != nil {
			return nil, err
		}
		if len(branchPat.steps) == 0 {
			continue
		}

		root := branchPat.steps[0]
		alt := alternativeSymbol{
			symbol:    root.symbol,
			isNamed:   root.isNamed,
			field:     altField,
			textMatch: root.textMatch,
			captureID: -1,
		}
		if root.field != 0 {
			alt.field = root.field
			// Field constraints are evaluated at branch selection time; keep the
			// branch root itself unconstrained after selection.
			branchPat.steps[0].field = 0
		}
		if len(branchPat.predicates) > 0 || len(branchPat.steps) > 1 || alt.field != 0 {
			alt.steps = make([]QueryStep, len(branchPat.steps))
			copy(alt.steps, branchPat.steps)
			alt.predicates = make([]QueryPredicate, len(branchPat.predicates))
			copy(alt.predicates, branchPat.predicates)
		} else {
			if len(root.captureIDs) > 0 {
				for _, capID := range root.captureIDs {
					p.addCaptureToAlternative(&alt, capID)
				}
			} else if root.captureID >= 0 {
				p.addCaptureToAlternative(&alt, root.captureID)
			}
		}
		alts = append(alts, alt)
	}

	if len(alts) == 0 {
		return nil, fmt.Errorf("query: empty alternation")
	}

	step := QueryStep{
		captureID:    -1,
		depth:        depth,
		alternatives: alts,
	}

	// Check for capture after ']'.
	p.skipWhitespaceAndComments()
	if quantifier, ok := p.readStepQuantifier(); ok {
		step.quantifier = quantifier
		p.skipWhitespaceAndComments()
	}
	for p.pos < len(p.input) && p.input[p.pos] == '@' {
		capName, err := p.readCapture()
		if err != nil {
			return nil, err
		}
		p.addCaptureToStep(&step, p.ensureCapture(capName))
		p.skipWhitespaceAndComments()
	}

	return &Pattern{steps: []QueryStep{step}}, nil
}

// parseStringPattern parses a "string" pattern for matching anonymous nodes.
func (p *queryParser) parseStringPattern(depth int) (*Pattern, error) {
	text, err := p.readString()
	if err != nil {
		return nil, err
	}

	step := QueryStep{
		captureID: -1,
		depth:     depth,
		textMatch: text,
	}

	// Check for capture after the string.
	p.skipWhitespaceAndComments()
	if quantifier, ok := p.readStepQuantifier(); ok {
		step.quantifier = quantifier
		p.skipWhitespaceAndComments()
	}
	for p.pos < len(p.input) && p.input[p.pos] == '@' {
		capName, err := p.readCapture()
		if err != nil {
			return nil, err
		}
		p.addCaptureToStep(&step, p.ensureCapture(capName))
		p.skipWhitespaceAndComments()
	}

	return &Pattern{steps: []QueryStep{step}}, nil
}

// parsePatternElement parses one query element at the given depth.
// Supported forms:
//   - (pattern ...)
//   - [alternation ...]
//   - "string"
//   - identifier / _ (shorthand single-node pattern)
func (p *queryParser) parsePatternElement(depth int, parentSymbolHint Symbol) (*Pattern, error) {
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("query: expected pattern element at end of input")
	}

	switch ch := p.input[p.pos]; {
	case ch == '(':
		return p.parsePattern(depth, parentSymbolHint)
	case ch == '[':
		return p.parseAlternationPattern(depth, parentSymbolHint)
	case ch == '"':
		return p.parseStringPattern(depth)
	case isIdentStart(ch):
		name, err := p.readIdentifier()
		if err != nil {
			return nil, err
		}
		return p.parseIdentifierPatternFromName(depth, name)
	default:
		return nil, fmt.Errorf("query: expected '(' or '[' or '\"' or identifier at position %d", p.pos)
	}
}

func (p *queryParser) stepFromIdentifierName(depth int, name string) (QueryStep, error) {
	sym, isNamed, err := p.resolveSymbol(name)
	if err != nil {
		return QueryStep{}, err
	}

	return QueryStep{
		symbol:    sym,
		isNamed:   isNamed,
		captureID: -1,
		depth:     depth,
	}, nil
}

func (p *queryParser) parseIdentifierPatternFromName(depth int, name string) (*Pattern, error) {
	step, err := p.stepFromIdentifierName(depth, name)
	if err != nil {
		return nil, err
	}

	p.skipWhitespaceAndComments()
	if quantifier, ok := p.readStepQuantifier(); ok {
		step.quantifier = quantifier
		p.skipWhitespaceAndComments()
	}
	for p.pos < len(p.input) && p.input[p.pos] == '@' {
		capName, err := p.readCapture()
		if err != nil {
			return nil, err
		}
		p.addCaptureToStep(&step, p.ensureCapture(capName))
		p.skipWhitespaceAndComments()
	}

	return &Pattern{steps: []QueryStep{step}}, nil
}

func (p *queryParser) parseFieldShorthandPattern(depth int) (*Pattern, error) {
	fieldName, err := p.readIdentifier()
	if err != nil {
		return nil, err
	}
	p.skipWhitespaceAndComments()
	if p.pos >= len(p.input) || p.input[p.pos] != ':' {
		return nil, fmt.Errorf("query: unexpected identifier %q at position %d", fieldName, p.pos)
	}
	p.pos++ // consume ':'
	p.skipWhitespaceAndComments()

	fieldID, err := p.resolveField(fieldName, 0, 0)
	if err != nil {
		return nil, err
	}

	childPat, err := p.parsePatternElement(depth+1, 0)
	if err != nil {
		return nil, err
	}
	if len(childPat.steps) > 0 {
		childPat.steps[0].field = fieldID
	}

	// Use a wildcard root so field constraints can still be represented in the
	// existing matcher shape.
	root := QueryStep{
		symbol:    0,
		isNamed:   false,
		captureID: -1,
		depth:     depth,
	}
	pat := &Pattern{steps: []QueryStep{root}}
	pat.steps = append(pat.steps, childPat.steps...)
	pat.predicates = append(pat.predicates, childPat.predicates...)
	return pat, nil
}

func (p *queryParser) validatePatternPredicates(pat *Pattern) error {
	if len(pat.predicates) == 0 {
		return nil
	}
	// Keep validation permissive. Runtime predicate evaluation rejects matches
	// when required captures are missing.
	return nil
}

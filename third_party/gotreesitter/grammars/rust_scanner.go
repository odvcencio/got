package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the rust grammar.
const (
	rustTokStringContent       = 0 // "string_content"
	rustTokRawStringStart      = 1 // "_raw_string_literal_start"
	rustTokRawStringContent    = 2 // "string_content" (raw variant)
	rustTokRawStringEnd        = 3 // "_raw_string_literal_end"
	rustTokFloatLiteral        = 4 // "float_literal"
	rustTokOuterDocMarker      = 5 // "outer_doc_comment_marker"
	rustTokInnerDocMarker      = 6 // "inner_doc_comment_marker"
	rustTokBlockCommentContent = 7 // "_block_comment_content"
	rustTokDocComment          = 8 // "doc_comment"
	rustTokErrorSentinel       = 9 // "_error_sentinel"
)

// Concrete symbol IDs from the generated rust grammar ExternalSymbols.
const (
	rustSymStringContent       gotreesitter.Symbol = 147
	rustSymRawStringStart      gotreesitter.Symbol = 148
	rustSymRawStringContent    gotreesitter.Symbol = 149
	rustSymRawStringEnd        gotreesitter.Symbol = 150
	rustSymFloatLiteral        gotreesitter.Symbol = 151
	rustSymOuterDocMarker      gotreesitter.Symbol = 152
	rustSymInnerDocMarker      gotreesitter.Symbol = 153
	rustSymBlockCommentContent gotreesitter.Symbol = 154
	rustSymDocComment          gotreesitter.Symbol = 155
	rustSymErrorSentinel       gotreesitter.Symbol = 156
)

// rustScannerState holds the opening hash count for raw string literals.
type rustScannerState struct {
	openingHashCount uint8
}

// RustExternalScanner implements gotreesitter.ExternalScanner for tree-sitter-rust.
//
// This is a Go port of the C external scanner from tree-sitter/tree-sitter-rust.
// The scanner handles 10 external tokens:
//   - String content (regular string escape sequences)
//   - Raw string literals (r#"..."# with hash counting for start/content/end)
//   - Float literals (disambiguation from integer + method call)
//   - Block comment content (nested /* */ comments)
//   - Doc comments (/// and //!)
//   - Outer/inner doc comment markers
//   - Error sentinel for error recovery detection
type RustExternalScanner struct{}

func (RustExternalScanner) Create() any {
	return &rustScannerState{}
}

func (RustExternalScanner) Destroy(payload any) {}

func (RustExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*rustScannerState)
	if len(buf) < 1 {
		return 0
	}
	buf[0] = s.openingHashCount
	return 1
}

func (RustExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*rustScannerState)
	s.openingHashCount = 0
	if len(buf) == 1 {
		s.openingHashCount = buf[0]
	}
}

func (RustExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	// If the error sentinel is valid, tree-sitter is in error recovery mode.
	// We cannot help recover, so bail out.
	if rustValid(validSymbols, rustTokErrorSentinel) {
		return false
	}

	s := payload.(*rustScannerState)

	// Block comment handling (content, inner/outer doc markers).
	if rustValid(validSymbols, rustTokBlockCommentContent) ||
		rustValid(validSymbols, rustTokInnerDocMarker) ||
		rustValid(validSymbols, rustTokOuterDocMarker) {
		return rustProcessBlockComment(lexer, validSymbols)
	}

	// String content (but not when float literal is also valid).
	if rustValid(validSymbols, rustTokStringContent) && !rustValid(validSymbols, rustTokFloatLiteral) {
		return rustProcessString(lexer)
	}

	// Line doc content.
	if rustValid(validSymbols, rustTokDocComment) {
		return rustProcessLineDocContent(lexer)
	}

	// Skip whitespace before checking remaining tokens.
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}

	// Raw string literal start.
	if rustValid(validSymbols, rustTokRawStringStart) {
		ch := lexer.Lookahead()
		if ch == 'r' || ch == 'b' || ch == 'c' {
			return rustScanRawStringStart(s, lexer)
		}
	}

	// Raw string literal content.
	if rustValid(validSymbols, rustTokRawStringContent) {
		return rustScanRawStringContent(s, lexer)
	}

	// Raw string literal end.
	if rustValid(validSymbols, rustTokRawStringEnd) && lexer.Lookahead() == '"' {
		return rustScanRawStringEnd(s, lexer)
	}

	// Float literal.
	if rustValid(validSymbols, rustTokFloatLiteral) && unicode.IsDigit(lexer.Lookahead()) {
		return rustProcessFloatLiteral(lexer)
	}

	return false
}

// ---------------------------------------------------------------------------
// String content
// ---------------------------------------------------------------------------

func rustProcessString(lexer *gotreesitter.ExternalLexer) bool {
	hasContent := false
	for {
		ch := lexer.Lookahead()
		if ch == '"' || ch == '\\' {
			break
		}
		if ch == 0 { // EOF
			return false
		}
		hasContent = true
		lexer.Advance(false)
	}
	lexer.SetResultSymbol(rustSymStringContent)
	lexer.MarkEnd()
	return hasContent
}

// ---------------------------------------------------------------------------
// Raw string literals
// ---------------------------------------------------------------------------

func rustScanRawStringStart(s *rustScannerState, lexer *gotreesitter.ExternalLexer) bool {
	ch := lexer.Lookahead()
	if ch == 'b' || ch == 'c' {
		lexer.Advance(false)
	}
	if lexer.Lookahead() != 'r' {
		return false
	}
	lexer.Advance(false)

	var openingHashCount uint8
	for lexer.Lookahead() == '#' {
		lexer.Advance(false)
		openingHashCount++
	}

	if lexer.Lookahead() != '"' {
		return false
	}
	lexer.Advance(false)
	s.openingHashCount = openingHashCount

	lexer.SetResultSymbol(rustSymRawStringStart)
	return true
}

func rustScanRawStringContent(s *rustScannerState, lexer *gotreesitter.ExternalLexer) bool {
	for {
		if lexer.Lookahead() == 0 { // EOF
			return false
		}
		if lexer.Lookahead() == '"' {
			lexer.MarkEnd()
			lexer.Advance(false)
			var hashCount uint8
			for lexer.Lookahead() == '#' && hashCount < s.openingHashCount {
				lexer.Advance(false)
				hashCount++
			}
			if hashCount == s.openingHashCount {
				lexer.SetResultSymbol(rustSymRawStringContent)
				return true
			}
		} else {
			lexer.Advance(false)
		}
	}
}

func rustScanRawStringEnd(s *rustScannerState, lexer *gotreesitter.ExternalLexer) bool {
	lexer.Advance(false) // consume the '"'
	for i := uint8(0); i < s.openingHashCount; i++ {
		lexer.Advance(false)
	}
	lexer.SetResultSymbol(rustSymRawStringEnd)
	return true
}

// ---------------------------------------------------------------------------
// Float literal
// ---------------------------------------------------------------------------

func rustIsNumChar(ch rune) bool {
	return ch == '_' || unicode.IsDigit(ch)
}

func rustProcessFloatLiteral(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(rustSymFloatLiteral)

	lexer.Advance(false)
	for rustIsNumChar(lexer.Lookahead()) {
		lexer.Advance(false)
	}

	hasFraction := false
	hasExponent := false

	if lexer.Lookahead() == '.' {
		hasFraction = true
		lexer.Advance(false)
		if unicode.IsLetter(lexer.Lookahead()) {
			// The dot is followed by a letter: 1.max(2) => not a float but an integer.
			return false
		}
		if lexer.Lookahead() == '.' {
			return false
		}
		for rustIsNumChar(lexer.Lookahead()) {
			lexer.Advance(false)
		}
	}

	lexer.MarkEnd()

	if lexer.Lookahead() == 'e' || lexer.Lookahead() == 'E' {
		hasExponent = true
		lexer.Advance(false)
		if lexer.Lookahead() == '+' || lexer.Lookahead() == '-' {
			lexer.Advance(false)
		}
		if !rustIsNumChar(lexer.Lookahead()) {
			return true
		}
		lexer.Advance(false)
		for rustIsNumChar(lexer.Lookahead()) {
			lexer.Advance(false)
		}
		lexer.MarkEnd()
	}

	if !hasExponent && !hasFraction {
		return false
	}

	ch := lexer.Lookahead()
	if ch != 'u' && ch != 'i' && ch != 'f' {
		return true
	}
	lexer.Advance(false)
	if !unicode.IsDigit(lexer.Lookahead()) {
		return true
	}
	for unicode.IsDigit(lexer.Lookahead()) {
		lexer.Advance(false)
	}

	lexer.MarkEnd()
	return true
}

// ---------------------------------------------------------------------------
// Line doc content
// ---------------------------------------------------------------------------

func rustProcessLineDocContent(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(rustSymDocComment)
	for {
		if lexer.Lookahead() == 0 { // EOF
			return true
		}
		if lexer.Lookahead() == '\n' {
			// Include the newline in the doc content node.
			// Line endings are useful for markdown injection.
			lexer.Advance(false)
			return true
		}
		lexer.Advance(false)
	}
}

// ---------------------------------------------------------------------------
// Block comment
// ---------------------------------------------------------------------------

// blockCommentState tracks the state machine for nested block comment parsing.
type blockCommentState int

const (
	bcLeftForwardSlash blockCommentState = iota
	bcLeftAsterisk
	bcContinuing
)

func rustProcessBlockComment(lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	first := lexer.Lookahead()

	// The first character is stored so we can safely advance inside
	// these if blocks. However, because we only store one, we can only
	// safely advance 1 time. Since there's a chance that an advance could
	// happen in one state, we must advance in all states to ensure that
	// the program ends up in a sane state prior to processing the block
	// comment if need be.
	if rustValid(validSymbols, rustTokInnerDocMarker) && first == '!' {
		lexer.SetResultSymbol(rustSymInnerDocMarker)
		lexer.Advance(false)
		return true
	}
	if rustValid(validSymbols, rustTokOuterDocMarker) && first == '*' {
		lexer.Advance(false)
		lexer.MarkEnd()
		// If the next token is a / that means that it's an empty block comment.
		if lexer.Lookahead() == '/' {
			return false
		}
		// If the next token is a * that means that this isn't a BLOCK_OUTER_DOC_MARKER
		// as BLOCK_OUTER_DOC_MARKER's only have 2 * not 3 or more.
		if lexer.Lookahead() != '*' {
			lexer.SetResultSymbol(rustSymOuterDocMarker)
			return true
		}
	} else {
		lexer.Advance(false)
	}

	if rustValid(validSymbols, rustTokBlockCommentContent) {
		state := bcContinuing
		nestingDepth := uint32(1)

		// Manually set the current state based on the first character.
		switch first {
		case '*':
			state = bcLeftAsterisk
			if lexer.Lookahead() == '/' {
				// This case can happen in an empty doc block comment
				// like /*!*/. The comment has no contents, so bail.
				return false
			}
		case '/':
			state = bcLeftForwardSlash
		default:
			state = bcContinuing
		}

		// For the purposes of actually parsing rust code, this
		// is incorrect as it considers an unterminated block comment
		// to be an error. However, for the purposes of syntax highlighting
		// this should be considered successful as otherwise you are not able
		// to syntax highlight a block of code prior to closing the
		// block comment.
		for lexer.Lookahead() != 0 && nestingDepth != 0 {
			// Set first to the current lookahead as that is the second character
			// as we force an advance in the above code when we are checking if we
			// need to handle a block comment inner or outer doc comment signifier node.
			current := lexer.Lookahead()
			switch state {
			case bcLeftForwardSlash:
				if current == '*' {
					nestingDepth++
				}
				state = bcContinuing
			case bcLeftAsterisk:
				if current == '*' {
					lexer.MarkEnd()
					// Stay in bcLeftAsterisk state.
				} else {
					if current == '/' {
						nestingDepth--
					}
					state = bcContinuing
				}
			case bcContinuing:
				lexer.MarkEnd()
				switch current {
				case '/':
					state = bcLeftForwardSlash
				case '*':
					state = bcLeftAsterisk
				}
			}
			lexer.Advance(false)
			if current == '/' && nestingDepth != 0 {
				lexer.MarkEnd()
			}
		}

		lexer.SetResultSymbol(rustSymBlockCommentContent)
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func rustValid(validSymbols []bool, idx int) bool {
	return idx >= 0 && idx < len(validSymbols) && validSymbols[idx]
}

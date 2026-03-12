package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the editorconfig grammar.
const (
	editorconfigTokEndOfFile         = 0
	editorconfigTokIntegerRangeStart = 1
)

const (
	editorconfigSymEndOfFile         gotreesitter.Symbol = 31
	editorconfigSymIntegerRangeStart gotreesitter.Symbol = 32
)

// EditorconfigExternalScanner handles EOF and integer-range detection for .editorconfig files.
type EditorconfigExternalScanner struct{}

func (EditorconfigExternalScanner) Create() any                           { return nil }
func (EditorconfigExternalScanner) Destroy(payload any)                   {}
func (EditorconfigExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (EditorconfigExternalScanner) Deserialize(payload any, buf []byte)   {}

func (EditorconfigExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	eofValid := editorconfigValid(validSymbols, editorconfigTokEndOfFile)
	intValid := editorconfigValid(validSymbols, editorconfigTokIntegerRangeStart)

	// Error recovery: both valid at once
	if eofValid && intValid {
		return false
	}

	if eofValid && lexer.Lookahead() == 0 {
		lexer.Advance(false)
		lexer.MarkEnd()
		lexer.SetResultSymbol(editorconfigSymEndOfFile)
		return true
	}

	if intValid {
		return editorconfigScanIntegerRange(lexer)
	}

	return false
}

func editorconfigScanIntegerRange(lexer *gotreesitter.ExternalLexer) bool {
	prev := lexer.Lookahead()
	lexer.Advance(false)

	if !isDigitRune(prev) && !(prev == '-' && isDigitRune(lexer.Lookahead())) {
		return false
	}

	for isDigitRune(lexer.Lookahead()) {
		lexer.Advance(false)
	}
	lexer.MarkEnd()

	prev = lexer.Lookahead()
	lexer.Advance(false)
	if !(prev == '.' && lexer.Lookahead() == '.') {
		return false
	}

	lexer.SetResultSymbol(editorconfigSymIntegerRangeStart)
	return true
}

func isDigitRune(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func editorconfigValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the less grammar.
const (
	lessTokDescendantOp = 0
)

const (
	lessSymDescendantOp gotreesitter.Symbol = 68
)

// LessExternalScanner handles the CSS descendant combinator for Less.
// Nearly identical to the SCSS descendant operator scanner.
type LessExternalScanner struct{}

func (LessExternalScanner) Create() any                           { return nil }
func (LessExternalScanner) Destroy(payload any)                   {}
func (LessExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (LessExternalScanner) Deserialize(payload any, buf []byte)   {}

func (LessExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !lessValid(validSymbols, lessTokDescendantOp) {
		return false
	}
	ch := lexer.Lookahead()
	if !isLessSpace(ch) {
		return false
	}
	lexer.Advance(true)
	for isLessSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}
	lexer.MarkEnd()
	lexer.SetResultSymbol(lessSymDescendantOp)

	next := lexer.Lookahead()
	if next == '#' || next == '.' || next == '[' || next == '-' ||
		next == '&' || next == '*' || unicode.IsLetter(next) || unicode.IsDigit(next) {
		return true
	}
	if next == ':' {
		lexer.Advance(false)
		if isLessSpace(lexer.Lookahead()) {
			return false
		}
		for {
			c := lexer.Lookahead()
			if c == ';' || c == '}' || c == 0 {
				return false
			}
			if c == '{' {
				return true
			}
			lexer.Advance(false)
		}
	}
	return false
}

func isLessSpace(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f'
}

func lessValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

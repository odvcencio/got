package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the uxntal grammar.
const (
	uxntalTokComment = 0
)

const (
	uxntalSymComment gotreesitter.Symbol = 285
)

// UxntalExternalScanner handles nestable ( ) Forth-style comments for Uxntal.
type UxntalExternalScanner struct{}

func (UxntalExternalScanner) Create() any                           { return nil }
func (UxntalExternalScanner) Destroy(payload any)                   {}
func (UxntalExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (UxntalExternalScanner) Deserialize(payload any, buf []byte)   {}

func (UxntalExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !uxntalValid(validSymbols, uxntalTokComment) {
		return false
	}
	if lexer.Lookahead() != '(' {
		return false
	}
	lexer.Advance(false)

	depth := 1
	for depth > 0 {
		ch := lexer.Lookahead()
		if ch == 0 {
			break
		}
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
		}
		lexer.Advance(false)
	}

	lexer.MarkEnd()
	lexer.SetResultSymbol(uxntalSymComment)
	return true
}

func uxntalValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

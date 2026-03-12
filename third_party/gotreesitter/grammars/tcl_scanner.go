package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the tcl grammar.
const (
	tclTokConcat    = 0
	tclTokImmediate = 1
)

const (
	tclSymConcat    gotreesitter.Symbol = 67
	tclSymImmediate gotreesitter.Symbol = 68
)

// TclExternalScanner handles concatenation detection for Tcl.
type TclExternalScanner struct{}

func (TclExternalScanner) Create() any                           { return nil }
func (TclExternalScanner) Destroy(payload any)                   {}
func (TclExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (TclExternalScanner) Deserialize(payload any, buf []byte)   {}

func (TclExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	ch := lexer.Lookahead()

	if tclValid(validSymbols, tclTokImmediate) && !unicode.IsSpace(ch) {
		lexer.SetResultSymbol(tclSymImmediate)
		return false
	}

	if tclValid(validSymbols, tclTokConcat) &&
		!unicode.IsSpace(ch) &&
		ch != ')' && ch != ':' && ch != '}' && ch != ']' {
		lexer.SetResultSymbol(tclSymConcat)
		return true
	}

	return false
}

func tclValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

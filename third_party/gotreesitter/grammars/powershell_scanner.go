package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the powershell grammar.
const (
	powershellTokStatementTerminator = 0
)

const (
	powershellSymStatementTerminator gotreesitter.Symbol = 232
)

// PowershellExternalScanner handles statement termination detection for PowerShell.
// A statement terminator is a zero-width token that fires when the next
// significant character is EOF, }, ;, ), or newline.
type PowershellExternalScanner struct{}

func (PowershellExternalScanner) Create() any                           { return nil }
func (PowershellExternalScanner) Destroy(payload any)                   {}
func (PowershellExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (PowershellExternalScanner) Deserialize(payload any, buf []byte)   {}

func (PowershellExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !powershellValid(validSymbols, powershellTokStatementTerminator) {
		return false
	}
	lexer.SetResultSymbol(powershellSymStatementTerminator)
	lexer.MarkEnd()

	for {
		ch := lexer.Lookahead()
		if ch == 0 || ch == '}' || ch == ';' || ch == ')' || ch == '\n' {
			return true
		}
		if !unicode.IsSpace(ch) {
			return false
		}
		lexer.Advance(true)
	}
}

func powershellValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

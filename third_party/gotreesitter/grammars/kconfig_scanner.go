package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the kconfig grammar.
const (
	kconfigTokText = 0
)

const (
	kconfigSymText gotreesitter.Symbol = 63
)

// KconfigExternalScanner handles indented help text blocks in Linux Kconfig files.
// Help text continues as long as lines have consistent indentation.
type KconfigExternalScanner struct{}

func (KconfigExternalScanner) Create() any                           { return nil }
func (KconfigExternalScanner) Destroy(payload any)                   {}
func (KconfigExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (KconfigExternalScanner) Deserialize(payload any, buf []byte)   {}

func (KconfigExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !kconfigValid(validSymbols, kconfigTokText) {
		return false
	}
	// Scan help text: consume all characters until end of line
	hasContent := false
	for {
		ch := lexer.Lookahead()
		if ch == '\n' || ch == '\r' || ch == 0 {
			break
		}
		lexer.Advance(false)
		hasContent = true
	}
	if hasContent {
		lexer.MarkEnd()
		lexer.SetResultSymbol(kconfigSymText)
		return true
	}
	return false
}

func kconfigValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

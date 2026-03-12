package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the wgsl grammar.
const (
	wgslTokBlockComment = 0
)

const (
	wgslSymBlockComment gotreesitter.Symbol = 135
)

// WgslExternalScanner handles nestable /* */ block comments for WGSL.
type WgslExternalScanner struct{}

func (WgslExternalScanner) Create() any                           { return nil }
func (WgslExternalScanner) Destroy(payload any)                   {}
func (WgslExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (WgslExternalScanner) Deserialize(payload any, buf []byte)   {}

func (WgslExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !wgslValid(validSymbols, wgslTokBlockComment) {
		return false
	}
	if lexer.Lookahead() != '/' {
		return false
	}
	lexer.Advance(false)
	if lexer.Lookahead() != '*' {
		return false
	}
	lexer.Advance(false)

	depth := 1
	afterStar := false
	for depth > 0 {
		ch := lexer.Lookahead()
		if ch == 0 {
			break
		}
		if ch == '*' {
			lexer.Advance(false)
			afterStar = true
			continue
		}
		if ch == '/' {
			lexer.Advance(false)
			if afterStar {
				depth--
				afterStar = false
			} else if lexer.Lookahead() == '*' {
				depth++
				lexer.Advance(false)
			}
			continue
		}
		lexer.Advance(false)
		afterStar = false
	}

	lexer.MarkEnd()
	lexer.SetResultSymbol(wgslSymBlockComment)
	return true
}

func wgslValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

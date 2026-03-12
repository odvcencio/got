package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the tablegen grammar.
const (
	tablegenTokMultilineComment = 0
)

const (
	tablegenSymMultilineComment gotreesitter.Symbol = 98
)

// TablegenExternalScanner handles nestable /* */ comments for TableGen.
type TablegenExternalScanner struct{}

func (TablegenExternalScanner) Create() any                           { return nil }
func (TablegenExternalScanner) Destroy(payload any)                   {}
func (TablegenExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (TablegenExternalScanner) Deserialize(payload any, buf []byte)   {}

func (TablegenExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !tablegenValid(validSymbols, tablegenTokMultilineComment) {
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
	lexer.SetResultSymbol(tablegenSymMultilineComment)
	return true
}

func tablegenValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

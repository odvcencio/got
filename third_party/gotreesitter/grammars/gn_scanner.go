package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the gn grammar.
const (
	gnTokStringContent = 0
)

const (
	gnSymStringContent gotreesitter.Symbol = 37
)

// GnExternalScanner handles string content for GN (Generate Ninja) build files.
// Scans content inside "..." strings, stopping at closing quote, escape
// sequences, and ${...} interpolations.
type GnExternalScanner struct{}

func (GnExternalScanner) Create() any                           { return nil }
func (GnExternalScanner) Destroy(payload any)                   {}
func (GnExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (GnExternalScanner) Deserialize(payload any, buf []byte)   {}

func (GnExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !gnValid(validSymbols, gnTokStringContent) {
		return false
	}
	hasContent := false
	for {
		lexer.MarkEnd()
		ch := lexer.Lookahead()
		switch ch {
		case '"', '\\':
			lexer.SetResultSymbol(gnSymStringContent)
			return hasContent
		case '$':
			lexer.Advance(false)
			if lexer.Lookahead() == '{' {
				lexer.SetResultSymbol(gnSymStringContent)
				return hasContent
			}
			// $x is also interpolation for single identifier chars
			if lexer.Lookahead() == '$' {
				// $$ is an escaped dollar, consume and continue
				lexer.Advance(false)
			}
			// Other $X — just continue
		case 0:
			return false
		default:
			lexer.Advance(false)
		}
		hasContent = true
	}
}

func gnValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

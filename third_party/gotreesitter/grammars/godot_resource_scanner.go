package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the godot_resource grammar.
const (
	godotResourceTokString = 0
)

const (
	godotResourceSymString gotreesitter.Symbol = 19
)

// GodotResourceExternalScanner handles multiline string literals in
// Godot .tres/.tscn resource files. Strings are "..." with \" escapes.
type GodotResourceExternalScanner struct{}

func (GodotResourceExternalScanner) Create() any                           { return nil }
func (GodotResourceExternalScanner) Destroy(payload any)                   {}
func (GodotResourceExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (GodotResourceExternalScanner) Deserialize(payload any, buf []byte)   {}

func (GodotResourceExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !godotResourceValid(validSymbols, godotResourceTokString) {
		return false
	}
	if lexer.Lookahead() != '"' {
		return false
	}
	lexer.Advance(false)

	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return false
		}
		if ch == '\\' {
			lexer.Advance(false)
			if lexer.Lookahead() != 0 {
				lexer.Advance(false)
			}
			continue
		}
		if ch == '"' {
			lexer.Advance(false)
			lexer.MarkEnd()
			lexer.SetResultSymbol(godotResourceSymString)
			return true
		}
		lexer.Advance(false)
	}
}

func godotResourceValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

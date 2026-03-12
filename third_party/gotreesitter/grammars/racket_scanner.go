package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the racket grammar.
const (
	racketTokHereStringBody = 0
)

const (
	racketSymHereStringBody gotreesitter.Symbol = 47
)

// RacketExternalScanner handles #<<DELIM here-strings for Racket.
// The scanner reads the terminator from the first line, then consumes
// subsequent lines until it finds one matching the terminator exactly.
type RacketExternalScanner struct{}

func (RacketExternalScanner) Create() any                           { return nil }
func (RacketExternalScanner) Destroy(payload any)                   {}
func (RacketExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (RacketExternalScanner) Deserialize(payload any, buf []byte)   {}

func (RacketExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if !racketValid(validSymbols, racketTokHereStringBody) {
		return false
	}

	// Read terminator (rest of current line)
	var terminator []rune
	for lexer.Lookahead() != '\n' && lexer.Lookahead() != 0 {
		terminator = append(terminator, lexer.Lookahead())
		lexer.Advance(false)
	}
	if lexer.Lookahead() == 0 {
		return false
	}
	// Skip the newline
	lexer.Advance(true)

	// Read lines until we find one matching terminator
	for {
		var line []rune
		for lexer.Lookahead() != '\n' && lexer.Lookahead() != 0 {
			line = append(line, lexer.Lookahead())
			lexer.Advance(false)
		}
		if racketRunesEqual(terminator, line) {
			lexer.SetResultSymbol(racketSymHereStringBody)
			return true
		}
		if lexer.Lookahead() == 0 {
			return false
		}
		// Skip newline
		lexer.Advance(true)
	}
}

func racketRunesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func racketValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

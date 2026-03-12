package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the SQL grammar (DerekStride/tree-sitter-sql).
const (
	sqlTokDollarTagStart = 0 // "_dollar_quoted_string_tag" — opening $tag$
	sqlTokContent        = 1 // "content" — body between tags
	sqlTokDollarTagEnd   = 2 // "_dollar_quoted_string_end_tag" — closing $tag$
)

// Concrete symbol IDs from the generated SQL grammar ExternalSymbols.
const (
	sqlSymDollarTagStart gotreesitter.Symbol = 285
	sqlSymContent        gotreesitter.Symbol = 286
	sqlSymDollarTagEnd   gotreesitter.Symbol = 287
)

// sqlScannerState stores the dollar-quote tag for matching the closing delimiter.
type sqlScannerState struct {
	tag string // empty when not inside a dollar-quoted string
}

// SqlExternalScanner implements gotreesitter.ExternalScanner for tree-sitter-sql.
//
// This is a Go port of the C external scanner from DerekStride/tree-sitter-sql.
// The scanner handles PostgreSQL dollar-quoted strings: $tag$..content..$tag$.
// It uses state to remember the opening tag so the closing tag can be matched.
type SqlExternalScanner struct{}

func (SqlExternalScanner) Create() any {
	return &sqlScannerState{}
}

func (SqlExternalScanner) Destroy(payload any) {}

func (SqlExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*sqlScannerState)
	if s.tag == "" {
		return 0
	}
	// Store tag + null terminator.
	tagLen := len(s.tag) + 1
	if tagLen > len(buf) {
		return 0
	}
	copy(buf, s.tag)
	buf[len(s.tag)] = 0
	return tagLen
}

func (SqlExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*sqlScannerState)
	s.tag = ""
	if len(buf) > 1 {
		// Find the null terminator.
		for i, b := range buf {
			if b == 0 {
				s.tag = string(buf[:i])
				return
			}
		}
		// No null terminator found — use the whole buffer.
		s.tag = string(buf)
	}
}

func (SqlExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*sqlScannerState)

	// Start tag: scan a $..$ tag and store it.
	if sqlValid(validSymbols, sqlTokDollarTagStart) && s.tag == "" {
		tag, ok := scanSqlDollarTag(lexer)
		if !ok {
			return false
		}
		s.tag = tag
		lexer.MarkEnd()
		lexer.SetResultSymbol(sqlSymDollarTagStart)
		return true
	}

	// End tag: scan for a matching $..$ tag.
	if sqlValid(validSymbols, sqlTokDollarTagEnd) && s.tag != "" {
		tag, ok := scanSqlDollarTag(lexer)
		if !ok || tag != s.tag {
			return false
		}
		s.tag = ""
		lexer.MarkEnd()
		lexer.SetResultSymbol(sqlSymDollarTagEnd)
		return true
	}

	// Content: scan the body between dollar-quote tags, stopping when we
	// see a potential closing tag (a '$' character).
	if sqlValid(validSymbols, sqlTokContent) && s.tag != "" {
		return scanSqlDollarContent(lexer)
	}

	return false
}

// scanSqlDollarTag scans a $identifier$ or $$ tag and returns the full tag
// string (including the $ delimiters). Returns ("", false) if the current
// position doesn't start a valid dollar tag.
func scanSqlDollarTag(lexer *gotreesitter.ExternalLexer) (string, bool) {
	if lexer.Lookahead() != '$' {
		return "", false
	}
	lexer.Advance(false)

	var tag []byte
	tag = append(tag, '$')

	// Tag identifier: [a-zA-Z_][a-zA-Z0-9_]* or empty (for $$).
	ch := lexer.Lookahead()
	if ch == '$' {
		// Empty tag: $$
		lexer.Advance(false)
		tag = append(tag, '$')
		return string(tag), true
	}

	if !isSqlTagStart(ch) {
		return "", false
	}

	for isSqlTagChar(lexer.Lookahead()) {
		tag = append(tag, byte(lexer.Lookahead()))
		lexer.Advance(false)
	}

	if lexer.Lookahead() != '$' {
		return "", false
	}
	lexer.Advance(false)
	tag = append(tag, '$')

	return string(tag), true
}

// scanSqlDollarContent scans the body of a dollar-quoted string, consuming
// everything until a '$' is encountered (which might be the start of the
// closing tag).
func scanSqlDollarContent(lexer *gotreesitter.ExternalLexer) bool {
	hasContent := false
	for {
		ch := lexer.Lookahead()
		if ch == '$' || ch == 0 {
			break
		}
		hasContent = true
		lexer.Advance(false)
	}
	if !hasContent {
		return false
	}
	lexer.MarkEnd()
	lexer.SetResultSymbol(sqlSymContent)
	return true
}

func isSqlTagStart(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isSqlTagChar(ch rune) bool {
	return isSqlTagStart(ch) || (ch >= '0' && ch <= '9')
}

func sqlValid(validSymbols []bool, idx int) bool {
	return idx >= 0 && idx < len(validSymbols) && validSymbols[idx]
}

package grammars

import gotreesitter "github.com/odvcencio/gotreesitter"

// External token indexes for the comment grammar.
const (
	commentTokName         = 0 // "name" — tag keyword like TODO, FIXME, NOTE
	commentTokInvalidToken = 1 // "invalid_token" — error recovery
)

// Concrete symbol IDs from the generated comment grammar ExternalSymbols.
const (
	commentSymName         gotreesitter.Symbol = 25
	commentSymInvalidToken gotreesitter.Symbol = 26
)

// CommentExternalScanner implements gotreesitter.ExternalScanner for tree-sitter-comment.
//
// The comment grammar (tree-sitter-comment) parses structured comment text
// such as "TODO: fix this" or "FIXME(user): description". The external
// scanner is responsible for producing the "name" token which represents
// a tag keyword (TODO, FIXME, NOTE, HACK, etc.).
//
// The scanner must be careful not to match arbitrary text as a "name",
// since the DFA handles regular text via _text_token1. A name is only
// returned when the scanned word is immediately followed by ':' or '(',
// indicating it forms part of a tag construct.
type CommentExternalScanner struct{}

func (CommentExternalScanner) Create() any                           { return nil }
func (CommentExternalScanner) Destroy(payload any)                   {}
func (CommentExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (CommentExternalScanner) Deserialize(payload any, buf []byte)   {}

func (CommentExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	// invalid_token: always decline; let the parser handle error recovery.
	if len(validSymbols) > commentTokInvalidToken && validSymbols[commentTokInvalidToken] &&
		!(len(validSymbols) > commentTokName && validSymbols[commentTokName]) {
		return false
	}

	// name: scan a tag keyword like TODO, FIXME, NOTE, etc.
	if len(validSymbols) > commentTokName && validSymbols[commentTokName] {
		return scanCommentName(lexer)
	}

	return false
}

// scanCommentName scans a name token (tag keyword). A name consists of
// alphanumeric characters, dots, hyphens, and underscores. To avoid
// conflicting with the DFA's _text_token1 production, the scanner only
// returns a name when it is immediately followed by ':' or '(', which
// indicates the word is a tag keyword rather than free text.
func scanCommentName(lexer *gotreesitter.ExternalLexer) bool {
	count := 0
	for {
		ch := lexer.Lookahead()
		if isCommentNameChar(ch) {
			lexer.Advance(false)
			count++
		} else {
			break
		}
	}
	if count == 0 {
		return false
	}

	// Only produce a name token when the next character indicates
	// this word is part of a tag construct, not free text.
	next := lexer.Lookahead()
	if next != ':' && next != '(' {
		return false
	}

	lexer.MarkEnd()
	lexer.SetResultSymbol(commentSymName)
	return true
}

// isCommentNameChar returns true for characters valid in a tag name:
// letters, digits, dot, hyphen, underscore.
func isCommentNameChar(ch rune) bool {
	if ch >= 'a' && ch <= 'z' {
		return true
	}
	if ch >= 'A' && ch <= 'Z' {
		return true
	}
	if ch >= '0' && ch <= '9' {
		return true
	}
	return ch == '.' || ch == '-' || ch == '_'
}

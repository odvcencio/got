package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the javascript grammar.
const (
	jsTokAutoSemicolon  = 0
	jsTokTemplateChars  = 1
	jsTokTernaryQmark   = 2
	jsTokHtmlComment    = 3
	jsTokLogicalOr      = 4
	jsTokEscapeSequence = 5
	jsTokRegexPattern   = 6
	jsTokJsxText        = 7
)

const (
	jsSymAutoSemicolon gotreesitter.Symbol = 129
	jsSymTemplateChars gotreesitter.Symbol = 130
	jsSymTernaryQmark  gotreesitter.Symbol = 131
	jsSymHtmlComment   gotreesitter.Symbol = 132
	jsSymJsxText       gotreesitter.Symbol = 133
)

// JavaScriptExternalScanner handles automatic semicolons, template strings,
// JSX text, ternary question marks, and HTML comments for JavaScript.
type JavaScriptExternalScanner struct{}

func (JavaScriptExternalScanner) Create() any                           { return nil }
func (JavaScriptExternalScanner) Destroy(payload any)                   {}
func (JavaScriptExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (JavaScriptExternalScanner) Deserialize(payload any, buf []byte)   {}

func (JavaScriptExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if jsValid(validSymbols, jsTokTemplateChars) {
		if jsValid(validSymbols, jsTokAutoSemicolon) {
			return false
		}
		return jsScanTemplateChars(lexer)
	}

	if jsValid(validSymbols, jsTokJsxText) {
		if jsScanJsxText(lexer) {
			return true
		}
	}

	if jsValid(validSymbols, jsTokAutoSemicolon) {
		scannedComment := false
		ret := jsScanAutoSemicolon(lexer, validSymbols, &scannedComment)
		if !ret && !scannedComment && jsValid(validSymbols, jsTokTernaryQmark) && lexer.Lookahead() == '?' {
			return jsScanTernaryQmark(lexer)
		}
		return ret
	}

	if jsValid(validSymbols, jsTokTernaryQmark) {
		return jsScanTernaryQmark(lexer)
	}

	if jsValid(validSymbols, jsTokHtmlComment) &&
		!jsValid(validSymbols, jsTokLogicalOr) &&
		!jsValid(validSymbols, jsTokEscapeSequence) &&
		!jsValid(validSymbols, jsTokRegexPattern) {
		return jsScanClosingComment(lexer)
	}

	return false
}

func jsScanTemplateChars(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(jsSymTemplateChars)
	hasContent := false
	for {
		lexer.MarkEnd()
		switch lexer.Lookahead() {
		case '`':
			return hasContent
		case 0:
			return false
		case '$':
			lexer.Advance(false)
			if lexer.Lookahead() == '{' {
				return hasContent
			}
		case '\\':
			return hasContent
		default:
			lexer.Advance(false)
			hasContent = true
		}
	}
}

func jsScanAutoSemicolon(lexer *gotreesitter.ExternalLexer, validSymbols []bool, scannedComment *bool) bool {
	lexer.SetResultSymbol(jsSymAutoSemicolon)
	lexer.MarkEnd()

	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return true
		}
		if ch == '}' {
			for {
				lexer.Advance(true)
				if !unicode.IsSpace(lexer.Lookahead()) {
					break
				}
			}
			if lexer.Lookahead() == ':' {
				return jsValid(validSymbols, jsTokLogicalOr)
			}
			return true
		}
		if !unicode.IsSpace(ch) {
			return false
		}
		if ch == '\n' {
			break
		}
		lexer.Advance(true)
	}

	lexer.Advance(true)

	if !jsScanWSAndComments(lexer, scannedComment) {
		return false
	}

	switch lexer.Lookahead() {
	case '`', ',', '.', ';', '*', '%', '>', '<', '=', '?', '^', '|', '&', '/', ':':
		return false
	case '{':
		// JavaScript has no func_sig_auto_semi, so no special handling here.
	case '(', '[':
		if jsValid(validSymbols, jsTokLogicalOr) {
			return false
		}
	case '+':
		lexer.Advance(true)
		return lexer.Lookahead() == '+'
	case '-':
		lexer.Advance(true)
		return lexer.Lookahead() == '-'
	case '!':
		lexer.Advance(true)
		return lexer.Lookahead() != '='
	case 'i':
		lexer.Advance(true)
		if lexer.Lookahead() != 'n' {
			return true
		}
		lexer.Advance(true)
		if !unicode.IsLetter(lexer.Lookahead()) {
			return false
		}
		stanceof := "stanceof"
		for i := 0; i < len(stanceof); i++ {
			if lexer.Lookahead() != rune(stanceof[i]) {
				return true
			}
			lexer.Advance(true)
		}
		if !unicode.IsLetter(lexer.Lookahead()) {
			return false
		}
	}

	return true
}

func jsScanWSAndComments(lexer *gotreesitter.ExternalLexer, scannedComment *bool) bool {
	for {
		for unicode.IsSpace(lexer.Lookahead()) {
			lexer.Advance(true)
		}
		if lexer.Lookahead() == '/' {
			lexer.Advance(true)
			if lexer.Lookahead() == '/' {
				lexer.Advance(true)
				for lexer.Lookahead() != 0 && lexer.Lookahead() != '\n' {
					lexer.Advance(true)
				}
				*scannedComment = true
			} else if lexer.Lookahead() == '*' {
				lexer.Advance(true)
				for lexer.Lookahead() != 0 {
					if lexer.Lookahead() == '*' {
						lexer.Advance(true)
						if lexer.Lookahead() == '/' {
							lexer.Advance(true)
							break
						}
					} else {
						lexer.Advance(true)
					}
				}
			} else {
				return false
			}
		} else {
			return true
		}
	}
}

func jsScanTernaryQmark(lexer *gotreesitter.ExternalLexer) bool {
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}

	if lexer.Lookahead() != '?' {
		return false
	}
	lexer.Advance(false)

	// Optional chaining
	if lexer.Lookahead() == '?' || lexer.Lookahead() == '.' {
		return false
	}

	lexer.MarkEnd()
	lexer.SetResultSymbol(jsSymTernaryQmark)

	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(false)
	}

	if lexer.Lookahead() == ':' || lexer.Lookahead() == ')' || lexer.Lookahead() == ',' {
		return false
	}

	if lexer.Lookahead() == '.' {
		lexer.Advance(false)
		if unicode.IsDigit(lexer.Lookahead()) {
			return true
		}
		return false
	}
	return true
}

func jsScanClosingComment(lexer *gotreesitter.ExternalLexer) bool {
	for unicode.IsSpace(lexer.Lookahead()) || lexer.Lookahead() == 0x2028 || lexer.Lookahead() == 0x2029 {
		lexer.Advance(true)
	}

	commentStart := "<!--"
	commentEnd := "-->"

	if lexer.Lookahead() == '<' {
		for i := 0; i < len(commentStart); i++ {
			if lexer.Lookahead() != rune(commentStart[i]) {
				return false
			}
			lexer.Advance(false)
		}
	} else if lexer.Lookahead() == '-' {
		for i := 0; i < len(commentEnd); i++ {
			if lexer.Lookahead() != rune(commentEnd[i]) {
				return false
			}
			lexer.Advance(false)
		}
	} else {
		return false
	}

	for lexer.Lookahead() != 0 && lexer.Lookahead() != '\n' &&
		lexer.Lookahead() != 0x2028 && lexer.Lookahead() != 0x2029 {
		lexer.Advance(false)
	}

	lexer.SetResultSymbol(jsSymHtmlComment)
	lexer.MarkEnd()
	return true
}

func jsScanJsxText(lexer *gotreesitter.ExternalLexer) bool {
	sawText := false
	atNewline := false

	for lexer.Lookahead() != 0 && lexer.Lookahead() != '<' && lexer.Lookahead() != '>' &&
		lexer.Lookahead() != '{' && lexer.Lookahead() != '}' && lexer.Lookahead() != '&' {
		isWS := unicode.IsSpace(lexer.Lookahead())
		if lexer.Lookahead() == '\n' {
			atNewline = true
		} else {
			atNewline = atNewline && isWS
			if !atNewline {
				sawText = true
			}
		}
		lexer.Advance(false)
	}

	lexer.MarkEnd()
	lexer.SetResultSymbol(jsSymJsxText)
	return sawText
}

func jsValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

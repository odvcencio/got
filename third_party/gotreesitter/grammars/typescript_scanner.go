package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the typescript grammar.
const (
	tsTokAutoSemicolon   = 0
	tsTokTemplateChars   = 1
	tsTokTernaryQmark    = 2
	tsTokHtmlComment     = 3
	tsTokLogicalOr       = 4
	tsTokEscapeSequence  = 5
	tsTokRegexPattern    = 6
	tsTokJsxText         = 7
	tsTokFuncSigAutoSemi = 8
	tsTokErrorRecovery   = 9
)

const (
	tsSymAutoSemicolon   gotreesitter.Symbol = 159
	tsSymTemplateChars   gotreesitter.Symbol = 160
	tsSymTernaryQmark    gotreesitter.Symbol = 161
	tsSymHtmlComment     gotreesitter.Symbol = 162
	tsSymJsxText         gotreesitter.Symbol = 163
	tsSymFuncSigAutoSemi gotreesitter.Symbol = 164
)

// TypeScriptExternalScanner handles automatic semicolons, template strings,
// JSX text, ternary question marks, and HTML comments for TypeScript.
type TypeScriptExternalScanner struct{}

func (TypeScriptExternalScanner) Create() any                           { return nil }
func (TypeScriptExternalScanner) Destroy(payload any)                   {}
func (TypeScriptExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (TypeScriptExternalScanner) Deserialize(payload any, buf []byte)   {}

func (TypeScriptExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if tsValid(validSymbols, tsTokTemplateChars) {
		if tsValid(validSymbols, tsTokAutoSemicolon) {
			return false
		}
		return tsScanTemplateChars(lexer)
	}

	if tsValid(validSymbols, tsTokJsxText) {
		if tsScanJsxText(lexer) {
			return true
		}
	}

	if tsValid(validSymbols, tsTokAutoSemicolon) || tsValid(validSymbols, tsTokFuncSigAutoSemi) {
		scannedComment := false
		ret := tsScanAutoSemicolon(lexer, validSymbols, &scannedComment)
		if !ret && !scannedComment && tsValid(validSymbols, tsTokTernaryQmark) && lexer.Lookahead() == '?' {
			return tsScanTernaryQmark(lexer)
		}
		return ret
	}

	if tsValid(validSymbols, tsTokTernaryQmark) {
		return tsScanTernaryQmark(lexer)
	}

	if tsValid(validSymbols, tsTokHtmlComment) &&
		!tsValid(validSymbols, tsTokLogicalOr) &&
		!tsValid(validSymbols, tsTokEscapeSequence) &&
		!tsValid(validSymbols, tsTokRegexPattern) {
		return tsScanClosingComment(lexer)
	}

	return false
}

func tsScanTemplateChars(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(tsSymTemplateChars)
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

func tsScanAutoSemicolon(lexer *gotreesitter.ExternalLexer, validSymbols []bool, scannedComment *bool) bool {
	lexer.SetResultSymbol(tsSymAutoSemicolon)
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
				return tsValid(validSymbols, tsTokLogicalOr)
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

	if !tsScanWSAndComments(lexer, scannedComment) {
		return false
	}

	switch lexer.Lookahead() {
	case '`', ',', '.', ';', '*', '%', '>', '<', '=', '?', '^', '|', '&', '/', ':':
		return false
	case '{':
		if tsValid(validSymbols, tsTokFuncSigAutoSemi) {
			return false
		}
	case '(', '[':
		if tsValid(validSymbols, tsTokLogicalOr) {
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

func tsScanWSAndComments(lexer *gotreesitter.ExternalLexer, scannedComment *bool) bool {
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

func tsScanTernaryQmark(lexer *gotreesitter.ExternalLexer) bool {
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
	lexer.SetResultSymbol(tsSymTernaryQmark)

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

func tsScanClosingComment(lexer *gotreesitter.ExternalLexer) bool {
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

	lexer.SetResultSymbol(tsSymHtmlComment)
	lexer.MarkEnd()
	return true
}

func tsScanJsxText(lexer *gotreesitter.ExternalLexer) bool {
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
	lexer.SetResultSymbol(tsSymJsxText)
	return sawText
}

func tsValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

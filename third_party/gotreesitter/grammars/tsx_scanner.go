package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the tsx grammar.
const (
	tsxTokAutoSemicolon   = 0
	tsxTokTemplateChars   = 1
	tsxTokTernaryQmark    = 2
	tsxTokHtmlComment     = 3
	tsxTokLogicalOr       = 4
	tsxTokEscapeSequence  = 5
	tsxTokRegexPattern    = 6
	tsxTokJsxText         = 7
	tsxTokFuncSigAutoSemi = 8
	tsxTokErrorRecovery   = 9
)

const (
	tsxSymAutoSemicolon   gotreesitter.Symbol = 165
	tsxSymTemplateChars   gotreesitter.Symbol = 166
	tsxSymTernaryQmark    gotreesitter.Symbol = 167
	tsxSymHtmlComment     gotreesitter.Symbol = 168
	tsxSymJsxText         gotreesitter.Symbol = 169
	tsxSymFuncSigAutoSemi gotreesitter.Symbol = 170
)

// TsxExternalScanner handles automatic semicolons, template strings,
// JSX text, ternary question marks, and HTML comments for TSX.
type TsxExternalScanner struct{}

func (TsxExternalScanner) Create() any                           { return nil }
func (TsxExternalScanner) Destroy(payload any)                   {}
func (TsxExternalScanner) Serialize(payload any, buf []byte) int { return 0 }
func (TsxExternalScanner) Deserialize(payload any, buf []byte)   {}

func (TsxExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	if tsxValid(validSymbols, tsxTokTemplateChars) {
		if tsxValid(validSymbols, tsxTokAutoSemicolon) {
			return false
		}
		return tsxScanTemplateChars(lexer)
	}

	if tsxValid(validSymbols, tsxTokJsxText) {
		if tsxScanJsxText(lexer) {
			return true
		}
	}

	if tsxValid(validSymbols, tsxTokAutoSemicolon) || tsxValid(validSymbols, tsxTokFuncSigAutoSemi) {
		scannedComment := false
		ret := tsxScanAutoSemicolon(lexer, validSymbols, &scannedComment)
		if !ret && !scannedComment && tsxValid(validSymbols, tsxTokTernaryQmark) && lexer.Lookahead() == '?' {
			return tsxScanTernaryQmark(lexer)
		}
		return ret
	}

	if tsxValid(validSymbols, tsxTokTernaryQmark) {
		return tsxScanTernaryQmark(lexer)
	}

	if tsxValid(validSymbols, tsxTokHtmlComment) &&
		!tsxValid(validSymbols, tsxTokLogicalOr) &&
		!tsxValid(validSymbols, tsxTokEscapeSequence) &&
		!tsxValid(validSymbols, tsxTokRegexPattern) {
		return tsxScanClosingComment(lexer)
	}

	return false
}

func tsxScanTemplateChars(lexer *gotreesitter.ExternalLexer) bool {
	lexer.SetResultSymbol(tsxSymTemplateChars)
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

func tsxScanAutoSemicolon(lexer *gotreesitter.ExternalLexer, validSymbols []bool, scannedComment *bool) bool {
	lexer.SetResultSymbol(tsxSymAutoSemicolon)
	lexer.MarkEnd()

	for {
		ch := lexer.Lookahead()
		if ch == 0 {
			return true
		}
		if ch == '}' {
			// Do not consume `}` while producing ASI; swallowing it truncates
			// surrounding blocks and can force premature error-accept paths.
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

	if !tsxScanWSAndComments(lexer, scannedComment) {
		return false
	}

	switch lexer.Lookahead() {
	case '`', ',', '.', ';', '*', '%', '>', '<', '=', '?', '^', '|', '&', '/', ':':
		return false
	case '{':
		if tsxValid(validSymbols, tsxTokFuncSigAutoSemi) {
			return false
		}
	case '(', '[':
		if tsxValid(validSymbols, tsxTokLogicalOr) {
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

func tsxScanWSAndComments(lexer *gotreesitter.ExternalLexer, scannedComment *bool) bool {
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

func tsxScanTernaryQmark(lexer *gotreesitter.ExternalLexer) bool {
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
	lexer.SetResultSymbol(tsxSymTernaryQmark)

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

func tsxScanClosingComment(lexer *gotreesitter.ExternalLexer) bool {
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

	lexer.SetResultSymbol(tsxSymHtmlComment)
	lexer.MarkEnd()
	return true
}

func tsxScanJsxText(lexer *gotreesitter.ExternalLexer) bool {
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
	lexer.SetResultSymbol(tsxSymJsxText)
	return sawText
}

func tsxValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

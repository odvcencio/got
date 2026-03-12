package grammars

import (
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the Vue grammar.
const (
	vueTokStartTagName        = 0
	vueTokScriptStartTagName  = 1
	vueTokStyleStartTagName   = 2
	vueTokEndTagName          = 3
	vueTokErroneousEndTagName = 4
	vueTokSelfClosingTagDelim = 5
	vueTokImplicitEndTag      = 6
	vueTokRawText             = 7
	vueTokComment             = 8
	vueTokTemplateStartTagName = 9
	vueTokTextFragment        = 10
	vueTokInterpolationText   = 11
)

const (
	vueSymStartTagName        gotreesitter.Symbol = 30
	vueSymScriptStartTagName  gotreesitter.Symbol = 31
	vueSymStyleStartTagName   gotreesitter.Symbol = 32
	vueSymEndTagName          gotreesitter.Symbol = 33
	vueSymErroneousEndTagName gotreesitter.Symbol = 34
	vueSymSelfClosingTagDelim gotreesitter.Symbol = 6
	vueSymImplicitEndTag      gotreesitter.Symbol = 35
	vueSymRawText             gotreesitter.Symbol = 36
	vueSymComment             gotreesitter.Symbol = 37
	vueSymTemplateStartTagName gotreesitter.Symbol = 38
	vueSymTextFragment        gotreesitter.Symbol = 39
	vueSymInterpolationText   gotreesitter.Symbol = 40
)

type vueState struct {
	tags []htmlTag
}

// VueExternalScanner handles HTML tag tracking plus Vue-specific text fragments and interpolation.
type VueExternalScanner struct{}

func (VueExternalScanner) Create() any { return &vueState{} }
func (VueExternalScanner) Destroy(payload any) {}

func (VueExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*vueState)
	return htmlSerializeTags(s.tags, buf)
}

func (VueExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*vueState)
	s.tags = htmlDeserializeTags(buf)
}

func (VueExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*vueState)
	lx := &goLexerAdapter{lexer}

	// Text fragment / interpolation text scanning
	isErrorRecovery := vueValid(validSymbols, vueTokStartTagName) && vueValid(validSymbols, vueTokRawText)
	if !isErrorRecovery && lexer.Lookahead() != '<' &&
		(vueValid(validSymbols, vueTokTextFragment) || vueValid(validSymbols, vueTokInterpolationText)) {
		return vueScanTextFragment(s, lexer, validSymbols)
	}

	if vueValid(validSymbols, vueTokRawText) && !vueValid(validSymbols, vueTokStartTagName) &&
		!vueValid(validSymbols, vueTokEndTagName) {
		return htmlScanRawText(lx, s.tags, vueSymRawText, lexer)
	}

	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}

	switch lexer.Lookahead() {
	case '<':
		lexer.MarkEnd()
		lexer.Advance(false)

		if lexer.Lookahead() == '!' {
			lexer.Advance(false)
			return htmlScanComment(lx, vueSymComment, lexer)
		}

		if vueValid(validSymbols, vueTokImplicitEndTag) {
			return htmlScanImplicitEndTag(lx, &s.tags, vueSymImplicitEndTag, lexer)
		}

	case 0:
		if vueValid(validSymbols, vueTokImplicitEndTag) {
			return htmlScanImplicitEndTag(lx, &s.tags, vueSymImplicitEndTag, lexer)
		}

	case '/':
		if vueValid(validSymbols, vueTokSelfClosingTagDelim) {
			return htmlScanSelfClosingDelim(lx, &s.tags, vueSymSelfClosingTagDelim, lexer)
		}

	default:
		if (vueValid(validSymbols, vueTokStartTagName) || vueValid(validSymbols, vueTokEndTagName)) &&
			!vueValid(validSymbols, vueTokRawText) {
			if vueValid(validSymbols, vueTokStartTagName) {
				return htmlScanStartTagName(lx, &s.tags, vueSymStartTagName, vueSymScriptStartTagName, vueSymStyleStartTagName, vueSymTemplateStartTagName, lexer)
			}
			return htmlScanEndTagName(lx, &s.tags, vueSymEndTagName, vueSymErroneousEndTagName, lexer)
		}
	}

	return false
}

func vueScanTextFragment(s *vueState, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	advancedOnce := false

	if !vueValid(validSymbols, vueTokComment) {
		for unicode.IsSpace(lexer.Lookahead()) {
			lexer.Advance(true)
		}
	}

	for lexer.Lookahead() != 0 {
		switch lexer.Lookahead() {
		case '<':
			lexer.MarkEnd()
			lexer.Advance(false)
			ch := lexer.Lookahead()
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '!' || ch == '?' || ch == '/' {
				goto loopExit
			}
			advancedOnce = true

		case '{':
			lexer.MarkEnd()
			lexer.Advance(false)
			if lexer.Lookahead() == '{' {
				goto loopExit
			}
			advancedOnce = true

		case '}':
			if vueValid(validSymbols, vueTokInterpolationText) {
				lexer.MarkEnd()
				lexer.Advance(false)
				if lexer.Lookahead() == '}' {
					lexer.SetResultSymbol(vueSymInterpolationText)
					return advancedOnce
				}
			} else {
				lexer.Advance(false)
				advancedOnce = true
			}

		case '\n':
			if vueValid(validSymbols, vueTokTextFragment) {
				lexer.MarkEnd()
				for unicode.IsSpace(lexer.Lookahead()) {
					if advancedOnce {
						lexer.Advance(false)
					} else {
						lexer.Advance(true)
					}
				}
				if lexer.Lookahead() == '<' || lexer.Lookahead() == '>' {
					goto loopExit
				}
			} else {
				lexer.Advance(false)
			}

		default:
			advancedOnce = advancedOnce || lexer.Lookahead() != '\n'
			lexer.Advance(false)
		}
	}

	if lexer.Lookahead() == 0 {
		return false
	}

loopExit:
	if advancedOnce {
		lexer.SetResultSymbol(vueSymTextFragment)
		return true
	}
	return false
}

func vueValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

package grammars

import (
	"sync"
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the Blade grammar.
const (
	bladeTokStartTagName           = 0
	bladeTokScriptStartTagName     = 1
	bladeTokStyleStartTagName      = 2
	bladeTokEndTagName             = 3
	bladeTokErroneousEndTagName    = 4
	bladeTokSelfClosingTagDelim    = 5
	bladeTokImplicitEndTag         = 6
	bladeTokRawText                = 7
	bladeTokComment                = 8
)

// bladeSyms caches resolved external symbol IDs for the blade grammar.
var bladeSyms struct {
	once                sync.Once
	startTagName        gotreesitter.Symbol
	scriptStartTagName  gotreesitter.Symbol
	styleStartTagName   gotreesitter.Symbol
	endTagName          gotreesitter.Symbol
	erroneousEndTagName gotreesitter.Symbol
	selfClosingTagDelim gotreesitter.Symbol
	implicitEndTag      gotreesitter.Symbol
	rawText             gotreesitter.Symbol
	comment             gotreesitter.Symbol
}

func resolveBladeSyms() {
	bladeSyms.once.Do(func() {
		lang := BladeLanguage()
		bladeSyms.startTagName = lang.ExternalSymbols[bladeTokStartTagName]
		bladeSyms.scriptStartTagName = lang.ExternalSymbols[bladeTokScriptStartTagName]
		bladeSyms.styleStartTagName = lang.ExternalSymbols[bladeTokStyleStartTagName]
		bladeSyms.endTagName = lang.ExternalSymbols[bladeTokEndTagName]
		bladeSyms.erroneousEndTagName = lang.ExternalSymbols[bladeTokErroneousEndTagName]
		bladeSyms.selfClosingTagDelim = lang.ExternalSymbols[bladeTokSelfClosingTagDelim]
		bladeSyms.implicitEndTag = lang.ExternalSymbols[bladeTokImplicitEndTag]
		bladeSyms.rawText = lang.ExternalSymbols[bladeTokRawText]
		bladeSyms.comment = lang.ExternalSymbols[bladeTokComment]
	})
}

type bladeState struct {
	tags []htmlTag
}

// BladeExternalScanner handles HTML tag tracking for Blade templates.
type BladeExternalScanner struct{}

func (BladeExternalScanner) Create() any { return &bladeState{} }
func (BladeExternalScanner) Destroy(payload any) {}

func (BladeExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*bladeState)
	return htmlSerializeTags(s.tags, buf)
}

func (BladeExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*bladeState)
	s.tags = htmlDeserializeTags(buf)
}

func (BladeExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*bladeState)
	lx := &goLexerAdapter{lexer}
	resolveBladeSyms()

	// Raw text in script/style tags
	if bladeValid(validSymbols, bladeTokRawText) && !bladeValid(validSymbols, bladeTokStartTagName) &&
		!bladeValid(validSymbols, bladeTokEndTagName) {
		return htmlScanRawText(lx, s.tags, bladeSyms.rawText, lexer)
	}

	// Skip whitespace
	for unicode.IsSpace(lexer.Lookahead()) {
		lexer.Advance(true)
	}

	switch lexer.Lookahead() {
	case '<':
		lexer.MarkEnd()
		lexer.Advance(false)

		if lexer.Lookahead() == '!' {
			lexer.Advance(false)
			return htmlScanComment(lx, bladeSyms.comment, lexer)
		}

		if bladeValid(validSymbols, bladeTokImplicitEndTag) {
			return htmlScanImplicitEndTag(lx, &s.tags, bladeSyms.implicitEndTag, lexer)
		}

	case 0:
		if bladeValid(validSymbols, bladeTokImplicitEndTag) {
			return htmlScanImplicitEndTag(lx, &s.tags, bladeSyms.implicitEndTag, lexer)
		}

	case '/':
		if bladeValid(validSymbols, bladeTokSelfClosingTagDelim) {
			return htmlScanSelfClosingDelim(lx, &s.tags, bladeSyms.selfClosingTagDelim, lexer)
		}

	default:
		if (bladeValid(validSymbols, bladeTokStartTagName) || bladeValid(validSymbols, bladeTokEndTagName)) &&
			!bladeValid(validSymbols, bladeTokRawText) {
			if bladeValid(validSymbols, bladeTokStartTagName) {
				return htmlScanStartTagName(lx, &s.tags, bladeSyms.startTagName, bladeSyms.scriptStartTagName, bladeSyms.styleStartTagName, 0, lexer)
			}
			return htmlScanEndTagName(lx, &s.tags, bladeSyms.endTagName, bladeSyms.erroneousEndTagName, lexer)
		}
	}

	return false
}

func bladeValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

// --- Shared HTML scanning helpers ---

type goLexerAdapter struct {
	l *gotreesitter.ExternalLexer
}

func (a *goLexerAdapter) lookahead() rune  { return a.l.Lookahead() }
func (a *goLexerAdapter) advance(skip bool) { a.l.Advance(skip) }
func (a *goLexerAdapter) markEnd()          { a.l.MarkEnd() }
func (a *goLexerAdapter) eof() bool         { return a.l.Lookahead() == 0 }

func htmlSerializeTags(tags []htmlTag, buf []byte) int {
	if len(buf) < 4 {
		return 0
	}
	tagCount := len(tags)
	if tagCount > 0xFFFF {
		tagCount = 0xFFFF
	}
	// Write tag count twice (serialized_count then total_count)
	buf[0] = byte(tagCount)
	buf[1] = byte(tagCount >> 8)
	buf[2] = byte(tagCount)
	buf[3] = byte(tagCount >> 8)
	size := 4
	serialized := 0
	for i := 0; i < tagCount; i++ {
		tag := tags[i]
		if tag.tagType == htmlTagCustom {
			nameLen := len(tag.customName)
			if nameLen > 255 {
				nameLen = 255
			}
			if size+2+nameLen >= len(buf) {
				break
			}
			buf[size] = byte(tag.tagType)
			size++
			buf[size] = byte(nameLen)
			size++
			copy(buf[size:], tag.customName[:nameLen])
			size += nameLen
		} else {
			if size+1 >= len(buf) {
				break
			}
			buf[size] = byte(tag.tagType)
			size++
		}
		serialized++
	}
	// Update serialized count
	buf[0] = byte(serialized)
	buf[1] = byte(serialized >> 8)
	return size
}

func htmlDeserializeTags(buf []byte) []htmlTag {
	if len(buf) < 4 {
		return nil
	}
	serializedCount := int(buf[0]) | int(buf[1])<<8
	size := 4
	var tags []htmlTag
	for i := 0; i < serializedCount && size < len(buf); i++ {
		tagType := htmlTagType(buf[size])
		size++
		if tagType == htmlTagCustom {
			if size >= len(buf) {
				break
			}
			nameLen := int(buf[size])
			size++
			if size+nameLen > len(buf) {
				break
			}
			name := string(buf[size : size+nameLen])
			size += nameLen
			tags = append(tags, htmlTag{tagType: htmlTagCustom, customName: name})
		} else {
			tags = append(tags, htmlTag{tagType: tagType})
		}
	}
	return tags
}

func htmlScanComment(lx htmlLexer, commentSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	if lx.lookahead() != '-' {
		return false
	}
	lx.advance(false)
	if lx.lookahead() != '-' {
		return false
	}
	lx.advance(false)

	dashes := uint32(0)
	for lx.lookahead() != 0 {
		switch lx.lookahead() {
		case '-':
			dashes++
		case '>':
			if dashes >= 2 {
				lexer.SetResultSymbol(commentSym)
				lx.advance(false)
				lx.markEnd()
				return true
			}
			dashes = 0
		default:
			dashes = 0
		}
		lx.advance(false)
	}
	return false
}

func htmlScanRawText(lx htmlLexer, tags []htmlTag, rawTextSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	if len(tags) == 0 {
		return false
	}
	lx.markEnd()

	lastTag := tags[len(tags)-1]
	var endDelimiter string
	if lastTag.tagType == htmlTagScript {
		endDelimiter = "</SCRIPT"
	} else {
		endDelimiter = "</STYLE"
	}

	delimIdx := 0
	for lx.lookahead() != 0 {
		ch := lx.lookahead()
		if ch >= 'a' && ch <= 'z' {
			ch = ch - 'a' + 'A'
		}
		if byte(ch) == endDelimiter[delimIdx] {
			delimIdx++
			if delimIdx == len(endDelimiter) {
				break
			}
			lx.advance(false)
		} else {
			delimIdx = 0
			lx.advance(false)
			lx.markEnd()
		}
	}

	lexer.SetResultSymbol(rawTextSym)
	return true
}

func htmlScanImplicitEndTag(lx htmlLexer, tags *[]htmlTag, implicitEndTagSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	var parent *htmlTag
	if len(*tags) > 0 {
		parent = &(*tags)[len(*tags)-1]
	}

	isClosingTag := false
	if lx.lookahead() == '/' {
		isClosingTag = true
		lx.advance(false)
	} else {
		if parent != nil && htmlTagIsVoid(parent) {
			*tags = (*tags)[:len(*tags)-1]
			lexer.SetResultSymbol(implicitEndTagSym)
			return true
		}
	}

	tagName := htmlScanTagName(lx)
	if len(tagName) == 0 && !lx.eof() {
		return false
	}

	nextTag := htmlTagForName(tagName)

	if isClosingTag {
		if len(*tags) > 0 && htmlTagEq(&(*tags)[len(*tags)-1], &nextTag) {
			return false
		}
		for i := len(*tags); i > 0; i-- {
			if (*tags)[i-1].tagType == nextTag.tagType {
				*tags = (*tags)[:len(*tags)-1]
				lexer.SetResultSymbol(implicitEndTagSym)
				return true
			}
		}
	} else if parent != nil &&
		(!htmlTagCanContain(parent, &nextTag) ||
			((parent.tagType == htmlTagHtml || parent.tagType == htmlTagHead || parent.tagType == htmlTagBody) && lx.eof())) {
		*tags = (*tags)[:len(*tags)-1]
		lexer.SetResultSymbol(implicitEndTagSym)
		return true
	}

	return false
}

func htmlScanSelfClosingDelim(lx htmlLexer, tags *[]htmlTag, selfClosingSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	lx.advance(false)
	if lx.lookahead() == '>' {
		lx.advance(false)
		if len(*tags) > 0 {
			*tags = (*tags)[:len(*tags)-1]
			lexer.SetResultSymbol(selfClosingSym)
		}
		return true
	}
	return false
}

func htmlScanStartTagName(lx htmlLexer, tags *[]htmlTag, startSym, scriptSym, styleSym, templateSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	tagName := htmlScanTagName(lx)
	if len(tagName) == 0 {
		return false
	}

	tag := htmlTagForName(tagName)
	*tags = append(*tags, tag)

	lx.markEnd()
	switch tag.tagType {
	case htmlTagScript:
		lexer.SetResultSymbol(scriptSym)
	case htmlTagStyle:
		lexer.SetResultSymbol(styleSym)
	case htmlTagTemplate:
		if templateSym != 0 {
			lexer.SetResultSymbol(templateSym)
		} else {
			lexer.SetResultSymbol(startSym)
		}
	default:
		lexer.SetResultSymbol(startSym)
	}
	return true
}

func htmlScanEndTagName(lx htmlLexer, tags *[]htmlTag, endSym, errEndSym gotreesitter.Symbol, lexer *gotreesitter.ExternalLexer) bool {
	tagName := htmlScanTagName(lx)
	if len(tagName) == 0 {
		return false
	}

	tag := htmlTagForName(tagName)
	lx.markEnd()
	if len(*tags) > 0 && htmlTagEq(&(*tags)[len(*tags)-1], &tag) {
		*tags = (*tags)[:len(*tags)-1]
		lexer.SetResultSymbol(endSym)
	} else {
		lexer.SetResultSymbol(errEndSym)
	}
	return true
}

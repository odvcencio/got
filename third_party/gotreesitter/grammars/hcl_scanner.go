package grammars

import (
	"encoding/binary"
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the HCL grammar.
const (
	hclTokQuotedTemplateStart        = 0
	hclTokQuotedTemplateEnd          = 1
	hclTokTemplateLiteralChunk       = 2
	hclTokTemplateInterpolationStart = 3
	hclTokTemplateInterpolationEnd   = 4
	hclTokTemplateDirectiveStart     = 5
	hclTokTemplateDirectiveEnd       = 6
	hclTokHeredocIdentifier          = 7
)

// Concrete symbol IDs from the generated HCL grammar.
const (
	hclSymQuotedTemplateStart        gotreesitter.Symbol = 48
	hclSymQuotedTemplateEnd          gotreesitter.Symbol = 49
	hclSymTemplateLiteralChunk       gotreesitter.Symbol = 50
	hclSymTemplateInterpolationStart gotreesitter.Symbol = 51
	hclSymTemplateInterpolationEnd   gotreesitter.Symbol = 52
	hclSymTemplateDirectiveStart     gotreesitter.Symbol = 53
	hclSymTemplateDirectiveEnd       gotreesitter.Symbol = 54
	hclSymHeredocIdentifier          gotreesitter.Symbol = 55
)

// hclContextType identifies the kind of template context being tracked.
type hclContextType uint32

const (
	hclCtxTemplateInterpolation hclContextType = iota
	hclCtxTemplateDirective
	hclCtxQuotedTemplate
	hclCtxHeredocTemplate
)

// hclContext represents one frame on the context stack.
type hclContext struct {
	ctxType           hclContextType
	heredocIdentifier string // non-empty only when ctxType == hclCtxHeredocTemplate
}

// hclState holds scanner state across parse calls.
type hclState struct {
	contextStack []hclContext
}

func (s *hclState) back() *hclContext {
	return &s.contextStack[len(s.contextStack)-1]
}

func (s *hclState) push(ctx hclContext) {
	s.contextStack = append(s.contextStack, ctx)
}

func (s *hclState) pop() {
	s.contextStack = s.contextStack[:len(s.contextStack)-1]
}

func (s *hclState) inContextType(ct hclContextType) bool {
	if len(s.contextStack) == 0 {
		return false
	}
	return s.back().ctxType == ct
}

func (s *hclState) inQuotedContext() bool   { return s.inContextType(hclCtxQuotedTemplate) }
func (s *hclState) inHeredocContext() bool   { return s.inContextType(hclCtxHeredocTemplate) }
func (s *hclState) inTemplateContext() bool  { return s.inQuotedContext() || s.inHeredocContext() }
func (s *hclState) inInterpolationContext() bool { return s.inContextType(hclCtxTemplateInterpolation) }
func (s *hclState) inDirectiveContext() bool { return s.inContextType(hclCtxTemplateDirective) }

// HclExternalScanner implements gotreesitter.ExternalScanner for the HCL grammar.
type HclExternalScanner struct{}

func (HclExternalScanner) Create() any { return &hclState{} }
func (HclExternalScanner) Destroy(payload any) {}

func (HclExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*hclState)
	if len(s.contextStack) > 127 {
		return 0
	}

	size := 0
	// Write context stack length as uint32 (little-endian to match C memcpy on LE).
	if size+4 > len(buf) {
		return 0
	}
	binary.LittleEndian.PutUint32(buf[size:], uint32(len(s.contextStack)))
	size += 4

	for i := range s.contextStack {
		ctx := &s.contextStack[i]
		idLen := len(ctx.heredocIdentifier)
		if idLen > 127 {
			return 0
		}
		// Need space for: uint32 type + uint32 idLen + id bytes.
		needed := 4 + 4 + idLen
		if size+needed > len(buf) {
			return 0
		}
		binary.LittleEndian.PutUint32(buf[size:], uint32(ctx.ctxType))
		size += 4
		binary.LittleEndian.PutUint32(buf[size:], uint32(idLen))
		size += 4
		copy(buf[size:], ctx.heredocIdentifier)
		size += idLen
	}
	return size
}

func (HclExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*hclState)
	s.contextStack = s.contextStack[:0]

	if len(buf) == 0 {
		return
	}

	size := 0
	if size+4 > len(buf) {
		return
	}
	stackLen := binary.LittleEndian.Uint32(buf[size:])
	size += 4

	for j := uint32(0); j < stackLen; j++ {
		if size+4 > len(buf) {
			return
		}
		ctxType := hclContextType(binary.LittleEndian.Uint32(buf[size:]))
		size += 4

		if size+4 > len(buf) {
			return
		}
		idLen := binary.LittleEndian.Uint32(buf[size:])
		size += 4

		var id string
		if idLen > 0 {
			if size+int(idLen) > len(buf) {
				return
			}
			id = string(buf[size : size+int(idLen)])
			size += int(idLen)
		}
		s.contextStack = append(s.contextStack, hclContext{
			ctxType:           ctxType,
			heredocIdentifier: id,
		})
	}
}

func (HclExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*hclState)

	// Skip whitespace, tracking whether a newline was seen.
	hasLeadingNewline := false
	for unicode.IsSpace(lexer.Lookahead()) {
		if lexer.Lookahead() == '\n' {
			hasLeadingNewline = true
		}
		lexer.Advance(true)
	}

	if lexer.Lookahead() == 0 {
		return false
	}

	// ---- Quoted template context ----
	if hclValid(validSymbols, hclTokQuotedTemplateStart) && !s.inQuotedContext() && lexer.Lookahead() == '"' {
		s.push(hclContext{ctxType: hclCtxQuotedTemplate})
		lexer.Advance(false)
		lexer.SetResultSymbol(hclSymQuotedTemplateStart)
		return true
	}
	if hclValid(validSymbols, hclTokQuotedTemplateEnd) && s.inQuotedContext() && lexer.Lookahead() == '"' {
		s.pop()
		lexer.Advance(false)
		lexer.SetResultSymbol(hclSymQuotedTemplateEnd)
		return true
	}

	// ---- Template interpolation ----
	if hclValid(validSymbols, hclTokTemplateInterpolationStart) &&
		hclValid(validSymbols, hclTokTemplateLiteralChunk) &&
		!s.inInterpolationContext() && lexer.Lookahead() == '$' {

		lexer.Advance(false)
		if lexer.Lookahead() == '{' {
			s.push(hclContext{ctxType: hclCtxTemplateInterpolation})
			lexer.Advance(false)
			lexer.SetResultSymbol(hclSymTemplateInterpolationStart)
			return true
		}
		// Escape sequence: $${ becomes literal chunk
		if lexer.Lookahead() == '$' {
			lexer.Advance(false)
			if lexer.Lookahead() == '{' {
				lexer.Advance(false)
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			}
		}
		lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
		return true
	}
	if hclValid(validSymbols, hclTokTemplateInterpolationEnd) && s.inInterpolationContext() && lexer.Lookahead() == '}' {
		s.pop()
		lexer.Advance(false)
		lexer.SetResultSymbol(hclSymTemplateInterpolationEnd)
		return true
	}

	// ---- Template directive ----
	if hclValid(validSymbols, hclTokTemplateDirectiveStart) &&
		hclValid(validSymbols, hclTokTemplateLiteralChunk) &&
		!s.inDirectiveContext() && lexer.Lookahead() == '%' {

		lexer.Advance(false)
		if lexer.Lookahead() == '{' {
			s.push(hclContext{ctxType: hclCtxTemplateDirective})
			lexer.Advance(false)
			lexer.SetResultSymbol(hclSymTemplateDirectiveStart)
			return true
		}
		// Escape sequence: %%{ becomes literal chunk
		if lexer.Lookahead() == '%' {
			lexer.Advance(false)
			if lexer.Lookahead() == '{' {
				lexer.Advance(false)
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			}
		}
		lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
		return true
	}
	if hclValid(validSymbols, hclTokTemplateDirectiveEnd) && s.inDirectiveContext() && lexer.Lookahead() == '}' {
		s.pop()
		lexer.Advance(false)
		lexer.SetResultSymbol(hclSymTemplateDirectiveEnd)
		return true
	}

	// ---- Heredoc identifier ----
	if hclValid(validSymbols, hclTokHeredocIdentifier) && !s.inHeredocContext() {
		// Scan a new heredoc identifier.
		var ident []byte
		for hclIsIdentChar(lexer.Lookahead()) {
			ident = append(ident, byte(lexer.Lookahead()))
			lexer.Advance(false)
		}
		s.push(hclContext{
			ctxType:           hclCtxHeredocTemplate,
			heredocIdentifier: string(ident),
		})
		lexer.SetResultSymbol(hclSymHeredocIdentifier)
		return true
	}
	if hclValid(validSymbols, hclTokHeredocIdentifier) && s.inHeredocContext() && hasLeadingNewline {
		expected := s.back().heredocIdentifier
		for i := 0; i < len(expected); i++ {
			if lexer.Lookahead() == rune(expected[i]) {
				lexer.Advance(false)
			} else {
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			}
		}
		// Check if the identifier is on a line of its own.
		lexer.MarkEnd()
		for unicode.IsSpace(lexer.Lookahead()) && lexer.Lookahead() != '\n' {
			lexer.Advance(false)
		}
		if lexer.Lookahead() == '\n' {
			s.pop()
			lexer.SetResultSymbol(hclSymHeredocIdentifier)
			return true
		}
		lexer.Advance(false)
		lexer.MarkEnd()
		lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
		return true
	}

	// ---- Template literal chunks ----

	// Handle escape sequences in quoted template context.
	if hclValid(validSymbols, hclTokTemplateLiteralChunk) && s.inQuotedContext() {
		if lexer.Lookahead() == '\\' {
			lexer.Advance(false)
			switch lexer.Lookahead() {
			case '"', 'n', 'r', 't', '\\':
				lexer.Advance(false)
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			case 'u':
				for i := 0; i < 4; i++ {
					lexer.Advance(false)
					if !hclIsHexDigit(lexer.Lookahead()) {
						return false
					}
				}
				lexer.Advance(false)
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			case 'U':
				for i := 0; i < 8; i++ {
					lexer.Advance(false)
					if !hclIsHexDigit(lexer.Lookahead()) {
						return false
					}
				}
				lexer.Advance(false)
				lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
				return true
			default:
				return false
			}
		}
	}

	// Handle all other characters in template contexts.
	if hclValid(validSymbols, hclTokTemplateLiteralChunk) && s.inTemplateContext() {
		lexer.Advance(false)
		lexer.SetResultSymbol(hclSymTemplateLiteralChunk)
		return true
	}

	return false
}

// hclValid checks whether a token index is valid in the current parse state.
func hclValid(vs []bool, i int) bool { return i < len(vs) && vs[i] }

// hclIsIdentChar returns true for characters allowed in HCL heredoc identifiers.
func hclIsIdentChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-'
}

// hclIsHexDigit returns true for hexadecimal digit characters.
func hclIsHexDigit(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

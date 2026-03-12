package grammars

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// External token indexes for the Mojo grammar (Python-like).
const (
	mojoTokNewline      = 0
	mojoTokIndent       = 1
	mojoTokDedent       = 2
	mojoTokStringStart  = 3
	mojoTokStringContent = 4
	mojoTokStringEnd    = 5
	mojoTokComment      = 6
	mojoTokCloseParen   = 7
	mojoTokCloseBracket = 8
	mojoTokCloseBrace   = 9
)

const (
	mojoSymNewline       gotreesitter.Symbol = 102
	mojoSymIndent        gotreesitter.Symbol = 103
	mojoSymDedent        gotreesitter.Symbol = 104
	mojoSymStringStart   gotreesitter.Symbol = 105
	mojoSymStringContent gotreesitter.Symbol = 106
	mojoSymStringEnd     gotreesitter.Symbol = 107
)

// MojoExternalScanner handles indent/dedent and string literals for Mojo.
// Mojo is Python-like; this reuses the pythonScannerState type.
type MojoExternalScanner struct{}

func (MojoExternalScanner) Create() any {
	return &pythonScannerState{indents: []uint16{0}}
}

func (MojoExternalScanner) Destroy(payload any) {}

func (MojoExternalScanner) Serialize(payload any, buf []byte) int {
	return PythonExternalScanner{}.Serialize(payload, buf)
}

func (MojoExternalScanner) Deserialize(payload any, buf []byte) {
	PythonExternalScanner{}.Deserialize(payload, buf)
}

func (MojoExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*pythonScannerState)
	if len(s.indents) == 0 {
		s.indents = append(s.indents, 0)
	}

	isValid := func(idx int) bool {
		return idx >= 0 && idx < len(validSymbols) && validSymbols[idx]
	}

	errorRecoveryMode := isValid(mojoTokStringContent) && isValid(mojoTokIndent)
	withinBrackets := isValid(mojoTokCloseBrace) || isValid(mojoTokCloseParen) || isValid(mojoTokCloseBracket)

	// String content scanning
	if isValid(mojoTokStringContent) && len(s.delimiters) > 0 && !errorRecoveryMode {
		delimiter := s.delimiters[len(s.delimiters)-1]
		endChar := delimiter.endChar()
		hasContent := false

		for lexer.Lookahead() != 0 {
			if lexer.Lookahead() == '\\' {
				if delimiter.isRaw() {
					lexer.Advance(false)
					if lexer.Lookahead() == endChar || lexer.Lookahead() == '\\' {
						lexer.Advance(false)
					}
					if lexer.Lookahead() == '\r' {
						lexer.Advance(false)
						if lexer.Lookahead() == '\n' {
							lexer.Advance(false)
						}
					} else if lexer.Lookahead() == '\n' {
						lexer.Advance(false)
					}
					continue
				}

				if delimiter.isBytes() {
					lexer.MarkEnd()
					lexer.Advance(false)
					if lexer.Lookahead() == 'N' || lexer.Lookahead() == 'u' || lexer.Lookahead() == 'U' {
						lexer.Advance(false)
					} else {
						lexer.SetResultSymbol(mojoSymStringContent)
						return hasContent
					}
				} else {
					lexer.MarkEnd()
					lexer.SetResultSymbol(mojoSymStringContent)
					return hasContent
				}
			} else if lexer.Lookahead() == endChar {
				if delimiter.isTriple() {
					lexer.MarkEnd()
					lexer.Advance(false)
					if lexer.Lookahead() == endChar {
						lexer.Advance(false)
						if lexer.Lookahead() == endChar {
							if hasContent {
								lexer.SetResultSymbol(mojoSymStringContent)
							} else {
								lexer.Advance(false)
								lexer.MarkEnd()
								s.delimiters = s.delimiters[:len(s.delimiters)-1]
								lexer.SetResultSymbol(mojoSymStringEnd)
								s.insideInterpolatedString = false
							}
							return true
						}
						lexer.MarkEnd()
						lexer.SetResultSymbol(mojoSymStringContent)
						return true
					}
					lexer.MarkEnd()
					lexer.SetResultSymbol(mojoSymStringContent)
					return true
				}

				if hasContent {
					lexer.SetResultSymbol(mojoSymStringContent)
				} else {
					lexer.Advance(false)
					s.delimiters = s.delimiters[:len(s.delimiters)-1]
					lexer.SetResultSymbol(mojoSymStringEnd)
					s.insideInterpolatedString = false
				}
				lexer.MarkEnd()
				return true
			} else if lexer.Lookahead() == '\n' && hasContent && !delimiter.isTriple() {
				return false
			}

			lexer.Advance(false)
			hasContent = true
		}
	}

	lexer.MarkEnd()

	foundEndOfLine := false
	var indentLength uint16
	firstCommentIndentLength := int32(-1)

	for {
		switch lexer.Lookahead() {
		case '\n':
			foundEndOfLine = true
			indentLength = 0
			lexer.Advance(true)
		case ' ':
			indentLength++
			lexer.Advance(true)
		case '\r', '\f':
			indentLength = 0
			lexer.Advance(true)
		case '\t':
			indentLength += 8
			lexer.Advance(true)
		case '#':
			if isValid(mojoTokIndent) || isValid(mojoTokDedent) || isValid(mojoTokNewline) {
				if !foundEndOfLine {
					return false
				}
				if firstCommentIndentLength == -1 {
					firstCommentIndentLength = int32(indentLength)
				}
				for lexer.Lookahead() != 0 && lexer.Lookahead() != '\n' {
					lexer.Advance(true)
				}
				lexer.Advance(true)
				indentLength = 0
				continue
			}
			goto mojoAfterIndentLoop
		case '\\':
			lexer.Advance(true)
			if lexer.Lookahead() == '\r' {
				lexer.Advance(true)
			}
			if lexer.Lookahead() == '\n' || lexer.Lookahead() == 0 {
				lexer.Advance(true)
			} else {
				return false
			}
		case 0:
			indentLength = 0
			foundEndOfLine = true
			goto mojoAfterIndentLoop
		default:
			goto mojoAfterIndentLoop
		}
	}

mojoAfterIndentLoop:
	if foundEndOfLine {
		currentIndent := s.indents[len(s.indents)-1]

		if isValid(mojoTokIndent) && indentLength > currentIndent {
			s.indents = append(s.indents, indentLength)
			lexer.SetResultSymbol(mojoSymIndent)
			return true
		}

		nextTokIsStringStart := lexer.Lookahead() == '"' || lexer.Lookahead() == '\'' || lexer.Lookahead() == '`'
		if (isValid(mojoTokDedent) ||
			(!isValid(mojoTokNewline) && !(isValid(mojoTokStringStart) && nextTokIsStringStart) && !withinBrackets)) &&
			indentLength < currentIndent &&
			!s.insideInterpolatedString &&
			firstCommentIndentLength < int32(currentIndent) {
			s.indents = s.indents[:len(s.indents)-1]
			lexer.SetResultSymbol(mojoSymDedent)
			return true
		}

		if isValid(mojoTokNewline) && !errorRecoveryMode {
			lexer.SetResultSymbol(mojoSymNewline)
			return true
		}
	}

	if firstCommentIndentLength == -1 && isValid(mojoTokStringStart) {
		var delimiter pyDelimiter
		hasFlags := false

		for lexer.Lookahead() != 0 {
			switch lexer.Lookahead() {
			case 'f', 'F':
				delimiter |= pyDelimFormat
			case 'r', 'R':
				delimiter |= pyDelimRaw
			case 'b', 'B':
				delimiter |= pyDelimBytes
			case 'u', 'U':
				// accepted prefix
			default:
				goto mojoAfterFlags
			}
			hasFlags = true
			lexer.Advance(false)
		}

	mojoAfterFlags:
		switch lexer.Lookahead() {
		case '`':
			delimiter |= pyDelimBackQuote
			lexer.Advance(false)
			lexer.MarkEnd()
		case '\'':
			delimiter |= pyDelimSingleQuote
			lexer.Advance(false)
			lexer.MarkEnd()
			if lexer.Lookahead() == '\'' {
				lexer.Advance(false)
				if lexer.Lookahead() == '\'' {
					lexer.Advance(false)
					lexer.MarkEnd()
					delimiter |= pyDelimTriple
				}
			}
		case '"':
			delimiter |= pyDelimDoubleQuote
			lexer.Advance(false)
			lexer.MarkEnd()
			if lexer.Lookahead() == '"' {
				lexer.Advance(false)
				if lexer.Lookahead() == '"' {
					lexer.Advance(false)
					lexer.MarkEnd()
					delimiter |= pyDelimTriple
				}
			}
		}

		if delimiter.endChar() != 0 {
			s.delimiters = append(s.delimiters, delimiter)
			lexer.SetResultSymbol(mojoSymStringStart)
			s.insideInterpolatedString = delimiter.isFormat()
			return true
		}
		if hasFlags {
			return false
		}
	}

	return false
}

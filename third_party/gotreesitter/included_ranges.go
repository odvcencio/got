package gotreesitter

import "sort"

type includedRangeTokenSource struct {
	base   TokenSource
	ranges []Range
	idx    int
}

func newIncludedRangeTokenSource(base TokenSource, ranges []Range) TokenSource {
	if base == nil || len(ranges) == 0 {
		return base
	}
	return &includedRangeTokenSource{
		base:   base,
		ranges: normalizeIncludedRanges(ranges),
	}
}

func normalizeIncludedRanges(ranges []Range) []Range {
	if len(ranges) == 0 {
		return nil
	}

	tmp := make([]Range, 0, len(ranges))
	for _, r := range ranges {
		if r.EndByte <= r.StartByte {
			continue
		}
		tmp = append(tmp, r)
	}
	if len(tmp) == 0 {
		return nil
	}

	sort.Slice(tmp, func(i, j int) bool {
		if tmp[i].StartByte != tmp[j].StartByte {
			return tmp[i].StartByte < tmp[j].StartByte
		}
		return tmp[i].EndByte < tmp[j].EndByte
	})

	out := make([]Range, 0, len(tmp))
	cur := tmp[0]
	for i := 1; i < len(tmp); i++ {
		r := tmp[i]
		if r.StartByte <= cur.EndByte {
			if r.EndByte > cur.EndByte {
				cur.EndByte = r.EndByte
				cur.EndPoint = r.EndPoint
			}
			continue
		}
		out = append(out, cur)
		cur = r
	}
	out = append(out, cur)
	return out
}

func (s *includedRangeTokenSource) SetParserState(state StateID) {
	if p, ok := s.base.(parserStateTokenSource); ok {
		p.SetParserState(state)
	}
}

func (s *includedRangeTokenSource) SetGLRStates(states []StateID) {
	if p, ok := s.base.(parserStateTokenSource); ok {
		p.SetGLRStates(states)
	}
}

func (s *includedRangeTokenSource) SupportsIncrementalReuse() bool {
	if s == nil || s.base == nil {
		return false
	}
	if dts, ok := s.base.(*dfaTokenSource); ok {
		return dts.language == nil || dts.language.ExternalScanner == nil
	}
	if reusable, ok := s.base.(IncrementalReuseTokenSource); ok {
		return reusable.SupportsIncrementalReuse()
	}
	return false
}

func (s *includedRangeTokenSource) Close() {
	if s == nil || s.base == nil {
		return
	}
	if closer, ok := s.base.(interface{ Close() }); ok {
		closer.Close()
	}
}

func (s *includedRangeTokenSource) Next() Token {
	return s.filterToken(Token{}, false)
}

func (s *includedRangeTokenSource) SkipToByte(offset uint32) Token {
	if skipper, ok := s.base.(ByteSkippableTokenSource); ok {
		return s.filterToken(skipper.SkipToByte(offset), true)
	}
	for {
		tok := s.Next()
		if tok.Symbol == 0 || tok.StartByte >= offset {
			return tok
		}
	}
}

func (s *includedRangeTokenSource) SkipToByteWithPoint(offset uint32, pt Point) Token {
	if skipper, ok := s.base.(PointSkippableTokenSource); ok {
		return s.filterToken(skipper.SkipToByteWithPoint(offset, pt), true)
	}
	return s.SkipToByte(offset)
}

func (s *includedRangeTokenSource) filterToken(tok Token, hasToken bool) Token {
	for {
		if !hasToken {
			tok = s.base.Next()
		}
		hasToken = false

		if tok.Symbol == 0 {
			return tok
		}
		if !s.advanceToMatchingRange(tok) {
			return Token{
				StartByte:  tok.EndByte,
				EndByte:    tok.EndByte,
				StartPoint: tok.EndPoint,
				EndPoint:   tok.EndPoint,
			}
		}

		r := s.ranges[s.idx]
		if tok.EndByte <= r.StartByte {
			if skipper, ok := s.base.(ByteSkippableTokenSource); ok {
				tok = skipper.SkipToByte(r.StartByte)
				hasToken = true
			}
			continue
		}
		if tok.StartByte >= r.EndByte {
			s.idx++
			hasToken = true
			continue
		}
		return tok
	}
}

func (s *includedRangeTokenSource) advanceToMatchingRange(tok Token) bool {
	for s.idx < len(s.ranges) && tok.StartByte >= s.ranges[s.idx].EndByte {
		s.idx++
	}
	return s.idx < len(s.ranges)
}

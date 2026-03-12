package gotreesitter

import "sync"

type parserScratch struct {
	merge      glrMergeScratch
	entries    glrEntryScratch
	gss        gssScratch
	tmpEntries []stackEntry
	glrStates  []StateID
	nodeLinks  []*Node
	stackPick  []int
	stackKeep  []bool
	stackCull  []stackCullKey
	stateKeep  []StateID
}

var parserScratchPool = sync.Pool{
	New: func() any {
		return &parserScratch{}
	},
}

func acquireParserScratch() *parserScratch {
	return parserScratchPool.Get().(*parserScratch)
}

func releaseParserScratch(s *parserScratch, skipGSSClear bool) {
	if s == nil {
		return
	}
	if len(s.merge.result) > 0 {
		clear(s.merge.result)
	}
	s.merge.result = s.merge.result[:0]
	if len(s.merge.slots) > 0 {
		s.merge.slots = s.merge.slots[:0]
	}
	s.merge.perKeyCap = 0
	if cap(s.tmpEntries) > 0 {
		buf := s.tmpEntries[:cap(s.tmpEntries)]
		clear(buf)
		if cap(buf) > maxRetainedStackEntryCap {
			s.tmpEntries = nil
		} else {
			s.tmpEntries = buf[:0]
		}
	}
	if cap(s.glrStates) > maxGLRStacks {
		s.glrStates = nil
	} else if len(s.glrStates) > 0 {
		s.glrStates = s.glrStates[:0]
	}
	const maxRetainedNodeLinkStack = 256 * 1024
	if cap(s.nodeLinks) > maxRetainedNodeLinkStack {
		s.nodeLinks = nil
	} else if len(s.nodeLinks) > 0 {
		s.nodeLinks = s.nodeLinks[:0]
	}
	const maxRetainedStackCullScratch = 256
	if cap(s.stackPick) > maxRetainedStackCullScratch {
		s.stackPick = nil
	} else if len(s.stackPick) > 0 {
		s.stackPick = s.stackPick[:0]
	}
	if cap(s.stackKeep) > maxRetainedStackCullScratch {
		s.stackKeep = nil
	} else if len(s.stackKeep) > 0 {
		s.stackKeep = s.stackKeep[:0]
	}
	if cap(s.stackCull) > maxRetainedStackCullScratch {
		s.stackCull = nil
	} else if len(s.stackCull) > 0 {
		s.stackCull = s.stackCull[:0]
	}
	if cap(s.stateKeep) > maxRetainedStackCullScratch {
		s.stateKeep = nil
	} else if len(s.stateKeep) > 0 {
		s.stateKeep = s.stateKeep[:0]
	}
	s.entries.reset()
	s.gss.skipClear = skipGSSClear
	s.gss.reset()
	parserScratchPool.Put(s)
}

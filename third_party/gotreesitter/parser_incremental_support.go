package gotreesitter

func (s *parseReuseState) markReused(node *Node, primary *nodeArena) {
	if s == nil {
		return
	}
	s.reusedAny = true
	if node == nil {
		return
	}
	s.arenaRefs = appendUniqueArenaRef(s.arenaRefs, node.ownerArena, primary)
}

func (s *parseReuseState) retainBorrowed(primary *nodeArena) []*nodeArena {
	if s == nil || !s.reusedAny || len(s.arenaRefs) == 0 {
		return nil
	}
	uniq := uniqueArenas(s.arenaRefs, primary)
	if len(uniq) == 0 {
		return nil
	}
	for _, a := range uniq {
		a.Retain()
	}
	return uniq
}

func (t *incrementalParseTiming) toProfile() IncrementalParseProfile {
	if t == nil {
		return IncrementalParseProfile{}
	}
	reparse := t.totalNanos - t.reuseNanos
	if reparse < 0 {
		reparse = 0
	}
	return IncrementalParseProfile{
		ReuseCursorNanos:   t.reuseNanos,
		ReparseNanos:       reparse,
		ReusedSubtrees:     t.reusedSubtrees,
		ReusedBytes:        t.reusedBytes,
		NewNodesAllocated:  t.newNodes,
		RecoverSearches:    t.recoverSearches,
		RecoverStateChecks: t.recoverStateChecks,
		RecoverStateSkips:  t.recoverStateSkips,
		RecoverSymbolSkips: t.recoverSymbolSkips,
		RecoverLookups:     t.recoverLookups,
		RecoverHits:        t.recoverHits,
		MaxStacksSeen:      t.maxStacksSeen,
		EntryScratchPeak:   t.entryScratchPeak,
	}
}

func appendUniqueArenaRef(refs []*nodeArena, arenaRef, exclude *nodeArena) []*nodeArena {
	if arenaRef == nil || arenaRef == exclude {
		return refs
	}
	for i := range refs {
		if refs[i] == arenaRef {
			return refs
		}
	}
	return append(refs, arenaRef)
}

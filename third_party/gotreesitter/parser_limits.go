package gotreesitter

import "sync/atomic"

func parseIterations(sourceLen int) int {
	return max(10_000, sourceLen*30)
}

// parseStackDepth returns the stack depth limit scaled to input size.
func parseStackDepth(sourceLen int) int {
	return max(1_000, sourceLen*2)
}

// parseNodeLimit returns the maximum number of Node allocations allowed.
// This is the hard ceiling that prevents OOM regardless of iteration count.
func parseNodeLimit(sourceLen int) int {
	// Keep the default budget high enough for large full-parse corpora so
	// correctness gates can run without relying on external scale overrides.
	// The 300k floor avoids premature truncation on small/medium inputs
	// during short-lived ambiguity spikes and malformed-input recovery.
	// The sourceLen*52 budget avoids first-pass node-limit retries on the
	// default synthetic Go full-parse workload while staying materially below
	// the diagnostic 2x override used for deeper corpus investigations.
	limit := max(300_000, sourceLen*52)
	scale := parseNodeLimitScaleFactor()
	if scale <= 1 {
		return limit
	}
	maxInt := int(^uint(0) >> 1)
	if limit > maxInt/scale {
		return maxInt
	}
	return limit * scale
}

func parseFullArenaNodeCapacity(sourceLen, hint int) int {
	base := nodeCapacityForClass(arenaClassFull)
	if hint > 0 {
		if hint < base {
			return base
		}
		limit := parseNodeLimit(sourceLen)
		if sourceLen <= 0 {
			return max(base, hint)
		}
		if hint > limit {
			return max(base, limit)
		}
		return hint
	}
	if sourceLen <= 0 {
		return base
	}
	// Conservative first-pass sizing. We refine this with adaptive hints
	// from observed full-parse node usage.
	estimate := sourceLen * 6
	const maxPreallocNodes = 1_500_000
	if estimate > maxPreallocNodes {
		estimate = maxPreallocNodes
	}
	return max(base, estimate)
}

func (p *Parser) fullArenaHintCapacity() int {
	if p == nil {
		return 0
	}
	return int(atomic.LoadUint32(&p.fullArenaHint))
}

func (p *Parser) recordFullArenaUsage(used int) {
	if p == nil || used <= 0 {
		return
	}
	target := used + used/4 // keep 25% headroom above observed peak.
	base := nodeCapacityForClass(arenaClassFull)
	if target < base {
		target = base
	}
	const maxHintNodes = 2_000_000
	if target > maxHintNodes {
		target = maxHintNodes
	}

	for {
		old := atomic.LoadUint32(&p.fullArenaHint)
		var next uint32
		if old == 0 {
			next = uint32(target)
		} else {
			blended := (int(old)*3 + target) / 4
			if blended < base {
				blended = base
			}
			next = uint32(blended)
		}
		if old == next || atomic.CompareAndSwapUint32(&p.fullArenaHint, old, next) {
			return
		}
	}
}

func parseFullEntryScratchCapacity(sourceLen int) int {
	if sourceLen <= 0 {
		return defaultStackEntrySlabCap
	}
	estimate := sourceLen * 12
	if estimate < defaultStackEntrySlabCap {
		estimate = defaultStackEntrySlabCap
	}
	// Keep initial scratch growth bounded; larger capacities are still
	// reached on demand and retained up to maxRetainedStackEntryCap.
	const maxPreallocEntries = 768 * 1024
	if estimate > maxPreallocEntries {
		estimate = maxPreallocEntries
	}
	return estimate
}

func parseIncrementalArenaNodeCapacity(sourceLen int) int {
	base := nodeCapacityForClass(arenaClassIncremental)
	if sourceLen <= 0 {
		return base
	}
	estimate := sourceLen * 4
	const maxPreallocNodes = 512 * 1024
	if estimate > maxPreallocNodes {
		estimate = maxPreallocNodes
	}
	return max(base, estimate)
}

func parseIncrementalEntryScratchCapacity(sourceLen int) int {
	if sourceLen <= 0 {
		return defaultStackEntrySlabCap
	}
	estimate := sourceLen * 8
	if estimate < defaultStackEntrySlabCap {
		estimate = defaultStackEntrySlabCap
	}
	const maxPreallocEntries = 256 * 1024
	if estimate > maxPreallocEntries {
		estimate = maxPreallocEntries
	}
	return estimate
}

//go:build perf

package gotreesitter

import "sync/atomic"

const (
	perfCountersEnabled = true
	perfMergeHistBins   = maxGLRStacks + 2
	perfForkHistBins    = 8 // 2..8, 9+
)

type perfCountersData struct {
	mergeCalls             atomic.Uint64
	mergeDeadPruned        atomic.Uint64
	mergePerKeyOverflow    atomic.Uint64
	mergeReplacements      atomic.Uint64
	stackEquivalentCalls   atomic.Uint64
	stackEquivalentTrue    atomic.Uint64
	stackEqHashMissSkips   atomic.Uint64
	stackCompareCalls      atomic.Uint64
	conflictRR             atomic.Uint64
	conflictRS             atomic.Uint64
	conflictOther          atomic.Uint64
	forkCount              atomic.Uint64
	firstConflictToken     atomic.Uint64
	maxConcurrentStacks    atomic.Uint64
	lexBytes               atomic.Uint64
	lexTokens              atomic.Uint64
	reuseNodesVisited      atomic.Uint64
	reuseNodesPushed       atomic.Uint64
	reuseNodesPopped       atomic.Uint64
	reuseCandidatesChecked atomic.Uint64
	reuseSuccesses         atomic.Uint64
	reuseLeafSuccesses     atomic.Uint64
	reuseNonLeafChecks     atomic.Uint64
	reuseNonLeafSuccesses  atomic.Uint64
	reuseNonLeafBytes      atomic.Uint64
	reuseNonLeafNoGoto     atomic.Uint64
	reuseNonLeafNoGotoTerm atomic.Uint64
	reuseNonLeafNoGotoNt   atomic.Uint64
	reuseNonLeafStateMiss  atomic.Uint64
	reuseNonLeafStateZero  atomic.Uint64
	mergeHashZero          atomic.Uint64
	globalCapCulls         atomic.Uint64
	globalCapCullDropped   atomic.Uint64
	reduceChainSteps       atomic.Uint64
	reduceChainMaxLen      atomic.Uint64
	reduceChainBreakMulti  atomic.Uint64
	reduceChainBreakShift  atomic.Uint64
	reduceChainBreakAccept atomic.Uint64
	parentChildPointers    atomic.Uint64
	extraNodes             atomic.Uint64
	errorNodes             atomic.Uint64
	mergeStacksInHist      [perfMergeHistBins]atomic.Uint64
	mergeAliveHist         [perfMergeHistBins]atomic.Uint64
	mergeOutHist           [perfMergeHistBins]atomic.Uint64
	forkActionsHist        [perfForkHistBins]atomic.Uint64
}

var perfCounters perfCountersData

type PerfCounters struct {
	MergeCalls             uint64
	MergeDeadPruned        uint64
	MergePerKeyOverflow    uint64
	MergeReplacements      uint64
	StackEquivalentCalls   uint64
	StackEquivalentTrue    uint64
	StackEqHashMissSkips   uint64
	StackCompareCalls      uint64
	ConflictRR             uint64
	ConflictRS             uint64
	ConflictOther          uint64
	ForkCount              uint64
	FirstConflictToken     uint64
	MaxConcurrentStacks    uint64
	LexBytes               uint64
	LexTokens              uint64
	ReuseNodesVisited      uint64
	ReuseNodesPushed       uint64
	ReuseNodesPopped       uint64
	ReuseCandidatesChecked uint64
	ReuseSuccesses         uint64
	ReuseLeafSuccesses     uint64
	ReuseNonLeafChecks     uint64
	ReuseNonLeafSuccesses  uint64
	ReuseNonLeafBytes      uint64
	ReuseNonLeafNoGoto     uint64
	ReuseNonLeafNoGotoTerm uint64
	ReuseNonLeafNoGotoNt   uint64
	ReuseNonLeafStateMiss  uint64
	ReuseNonLeafStateZero  uint64
	MergeHashZero          uint64
	GlobalCapCulls         uint64
	GlobalCapCullDropped   uint64
	ReduceChainSteps       uint64
	ReduceChainMaxLen      uint64
	ReduceChainBreakMulti  uint64
	ReduceChainBreakShift  uint64
	ReduceChainBreakAccept uint64
	ParentChildPointers    uint64
	ExtraNodes             uint64
	ErrorNodes             uint64
	MergeStacksInHist      [perfMergeHistBins]uint64
	MergeAliveHist         [perfMergeHistBins]uint64
	MergeOutHist           [perfMergeHistBins]uint64
	ForkActionsHist        [perfForkHistBins]uint64
}

func ResetPerfCounters() {
	perfCounters.mergeCalls.Store(0)
	perfCounters.mergeDeadPruned.Store(0)
	perfCounters.mergePerKeyOverflow.Store(0)
	perfCounters.mergeReplacements.Store(0)
	perfCounters.stackEquivalentCalls.Store(0)
	perfCounters.stackEquivalentTrue.Store(0)
	perfCounters.stackEqHashMissSkips.Store(0)
	perfCounters.stackCompareCalls.Store(0)
	perfCounters.conflictRR.Store(0)
	perfCounters.conflictRS.Store(0)
	perfCounters.conflictOther.Store(0)
	perfCounters.forkCount.Store(0)
	perfCounters.firstConflictToken.Store(0)
	perfCounters.maxConcurrentStacks.Store(0)
	perfCounters.lexBytes.Store(0)
	perfCounters.lexTokens.Store(0)
	perfCounters.reuseNodesVisited.Store(0)
	perfCounters.reuseNodesPushed.Store(0)
	perfCounters.reuseNodesPopped.Store(0)
	perfCounters.reuseCandidatesChecked.Store(0)
	perfCounters.reuseSuccesses.Store(0)
	perfCounters.reuseLeafSuccesses.Store(0)
	perfCounters.reuseNonLeafChecks.Store(0)
	perfCounters.reuseNonLeafSuccesses.Store(0)
	perfCounters.reuseNonLeafBytes.Store(0)
	perfCounters.reuseNonLeafNoGoto.Store(0)
	perfCounters.reuseNonLeafNoGotoTerm.Store(0)
	perfCounters.reuseNonLeafNoGotoNt.Store(0)
	perfCounters.reuseNonLeafStateMiss.Store(0)
	perfCounters.reuseNonLeafStateZero.Store(0)
	perfCounters.parentChildPointers.Store(0)
	perfCounters.extraNodes.Store(0)
	perfCounters.errorNodes.Store(0)
	for i := range perfCounters.mergeStacksInHist {
		perfCounters.mergeStacksInHist[i].Store(0)
	}
	for i := range perfCounters.mergeAliveHist {
		perfCounters.mergeAliveHist[i].Store(0)
	}
	perfCounters.mergeHashZero.Store(0)
	perfCounters.globalCapCulls.Store(0)
	perfCounters.globalCapCullDropped.Store(0)
	perfCounters.reduceChainSteps.Store(0)
	perfCounters.reduceChainMaxLen.Store(0)
	perfCounters.reduceChainBreakMulti.Store(0)
	perfCounters.reduceChainBreakShift.Store(0)
	perfCounters.reduceChainBreakAccept.Store(0)
	for i := range perfCounters.mergeOutHist {
		perfCounters.mergeOutHist[i].Store(0)
	}
	for i := range perfCounters.forkActionsHist {
		perfCounters.forkActionsHist[i].Store(0)
	}
}

func PerfCountersSnapshot() PerfCounters {
	var out PerfCounters
	out.MergeCalls = perfCounters.mergeCalls.Load()
	out.MergeDeadPruned = perfCounters.mergeDeadPruned.Load()
	out.MergePerKeyOverflow = perfCounters.mergePerKeyOverflow.Load()
	out.MergeReplacements = perfCounters.mergeReplacements.Load()
	out.StackEquivalentCalls = perfCounters.stackEquivalentCalls.Load()
	out.StackEquivalentTrue = perfCounters.stackEquivalentTrue.Load()
	out.StackEqHashMissSkips = perfCounters.stackEqHashMissSkips.Load()
	out.StackCompareCalls = perfCounters.stackCompareCalls.Load()
	out.ConflictRR = perfCounters.conflictRR.Load()
	out.ConflictRS = perfCounters.conflictRS.Load()
	out.ConflictOther = perfCounters.conflictOther.Load()
	out.ForkCount = perfCounters.forkCount.Load()
	out.FirstConflictToken = perfCounters.firstConflictToken.Load()
	out.MaxConcurrentStacks = perfCounters.maxConcurrentStacks.Load()
	out.LexBytes = perfCounters.lexBytes.Load()
	out.LexTokens = perfCounters.lexTokens.Load()
	out.ReuseNodesVisited = perfCounters.reuseNodesVisited.Load()
	out.ReuseNodesPushed = perfCounters.reuseNodesPushed.Load()
	out.ReuseNodesPopped = perfCounters.reuseNodesPopped.Load()
	out.ReuseCandidatesChecked = perfCounters.reuseCandidatesChecked.Load()
	out.ReuseSuccesses = perfCounters.reuseSuccesses.Load()
	out.ReuseLeafSuccesses = perfCounters.reuseLeafSuccesses.Load()
	out.ReuseNonLeafChecks = perfCounters.reuseNonLeafChecks.Load()
	out.ReuseNonLeafSuccesses = perfCounters.reuseNonLeafSuccesses.Load()
	out.ReuseNonLeafBytes = perfCounters.reuseNonLeafBytes.Load()
	out.ReuseNonLeafNoGoto = perfCounters.reuseNonLeafNoGoto.Load()
	out.ReuseNonLeafNoGotoTerm = perfCounters.reuseNonLeafNoGotoTerm.Load()
	out.ReuseNonLeafNoGotoNt = perfCounters.reuseNonLeafNoGotoNt.Load()
	out.ReuseNonLeafStateMiss = perfCounters.reuseNonLeafStateMiss.Load()
	out.ReuseNonLeafStateZero = perfCounters.reuseNonLeafStateZero.Load()
	for i := range out.MergeStacksInHist {
		out.MergeStacksInHist[i] = perfCounters.mergeStacksInHist[i].Load()
	}
	for i := range out.MergeAliveHist {
		out.MergeAliveHist[i] = perfCounters.mergeAliveHist[i].Load()
	}
	out.MergeHashZero = perfCounters.mergeHashZero.Load()
	out.GlobalCapCulls = perfCounters.globalCapCulls.Load()
	out.GlobalCapCullDropped = perfCounters.globalCapCullDropped.Load()
	out.ReduceChainSteps = perfCounters.reduceChainSteps.Load()
	out.ReduceChainMaxLen = perfCounters.reduceChainMaxLen.Load()
	out.ReduceChainBreakMulti = perfCounters.reduceChainBreakMulti.Load()
	out.ReduceChainBreakShift = perfCounters.reduceChainBreakShift.Load()
	out.ReduceChainBreakAccept = perfCounters.reduceChainBreakAccept.Load()
	out.ParentChildPointers = perfCounters.parentChildPointers.Load()
	out.ExtraNodes = perfCounters.extraNodes.Load()
	out.ErrorNodes = perfCounters.errorNodes.Load()
	for i := range out.MergeOutHist {
		out.MergeOutHist[i] = perfCounters.mergeOutHist[i].Load()
	}
	for i := range out.ForkActionsHist {
		out.ForkActionsHist[i] = perfCounters.forkActionsHist[i].Load()
	}
	return out
}

func perfRecordMergeCall(stacksIn int) {
	perfCounters.mergeCalls.Add(1)
	perfCounters.mergeStacksInHist[perfMergeHistBin(stacksIn)].Add(1)
}

func perfRecordMergeAlive(alive, dead int) {
	if dead > 0 {
		perfCounters.mergeDeadPruned.Add(uint64(dead))
	}
	perfCounters.mergeAliveHist[perfMergeHistBin(alive)].Add(1)
}

func perfRecordMergeOut(stacksOut int) {
	perfCounters.mergeOutHist[perfMergeHistBin(stacksOut)].Add(1)
}

func perfRecordMergeHashZero() {
	perfCounters.mergeHashZero.Add(1)
}

func perfRecordGlobalCapCull(before, cap int) {
	perfCounters.globalCapCulls.Add(1)
	if before > cap {
		perfCounters.globalCapCullDropped.Add(uint64(before - cap))
	}
}

func perfRecordMergePerKeyOverflow() {
	perfCounters.mergePerKeyOverflow.Add(1)
}

func perfRecordMergeReplacement() {
	perfCounters.mergeReplacements.Add(1)
}

func perfRecordStackEquivalentCall() {
	perfCounters.stackEquivalentCalls.Add(1)
}

func perfRecordStackEquivalentTrue() {
	perfCounters.stackEquivalentTrue.Add(1)
}

func perfRecordStackEquivalentHashMissSkip() {
	perfCounters.stackEqHashMissSkips.Add(1)
}

func perfRecordStackCompare() {
	perfCounters.stackCompareCalls.Add(1)
}

func perfRecordConflictRR() {
	perfCounters.conflictRR.Add(1)
}

func perfRecordConflictRS() {
	perfCounters.conflictRS.Add(1)
}

func perfRecordConflictOther() {
	perfCounters.conflictOther.Add(1)
}

func perfRecordFork(actionCount int, tokenPos uint64) {
	perfCounters.forkCount.Add(1)
	perfCounters.forkActionsHist[perfForkHistBin(actionCount)].Add(1)
	if tokenPos == 0 {
		return
	}
	perfCounters.firstConflictToken.CompareAndSwap(0, tokenPos)
}

func perfRecordMaxConcurrentStacks(n int) {
	if n <= 0 {
		return
	}
	target := uint64(n)
	for {
		prev := perfCounters.maxConcurrentStacks.Load()
		if target <= prev {
			return
		}
		if perfCounters.maxConcurrentStacks.CompareAndSwap(prev, target) {
			return
		}
	}
}

func perfRecordLexed(bytes, tokens int) {
	if bytes > 0 {
		perfCounters.lexBytes.Add(uint64(bytes))
	}
	if tokens > 0 {
		perfCounters.lexTokens.Add(uint64(tokens))
	}
}

func perfRecordReuseVisited() {
	perfCounters.reuseNodesVisited.Add(1)
}

func perfRecordReusePushed(n int) {
	if n > 0 {
		perfCounters.reuseNodesPushed.Add(uint64(n))
	}
}

func perfRecordReusePopped() {
	perfCounters.reuseNodesPopped.Add(1)
}

func perfRecordReuseCandidates(n int) {
	if n > 0 {
		perfCounters.reuseCandidatesChecked.Add(uint64(n))
	}
}

func perfRecordReuseSuccess() {
	perfCounters.reuseSuccesses.Add(1)
}

func perfRecordReuseLeafSuccess() {
	perfCounters.reuseLeafSuccesses.Add(1)
}

func perfRecordReuseNonLeafCheck() {
	perfCounters.reuseNonLeafChecks.Add(1)
}

func perfRecordReuseNonLeafSuccess(bytes uint32) {
	perfCounters.reuseNonLeafSuccesses.Add(1)
	if bytes > 0 {
		perfCounters.reuseNonLeafBytes.Add(uint64(bytes))
	}
}

func perfRecordReuseNonLeafNoGoto() {
	perfCounters.reuseNonLeafNoGoto.Add(1)
}

func perfRecordReuseNonLeafNoGotoTerminal() {
	perfCounters.reuseNonLeafNoGotoTerm.Add(1)
}

func perfRecordReuseNonLeafNoGotoNonTerminal() {
	perfCounters.reuseNonLeafNoGotoNt.Add(1)
}

func perfRecordReuseNonLeafStateMiss() {
	perfCounters.reuseNonLeafStateMiss.Add(1)
}

func perfRecordReuseNonLeafStateZero() {
	perfCounters.reuseNonLeafStateZero.Add(1)
}

func perfRecordReduceChainStep(chainLen int) {
	perfCounters.reduceChainSteps.Add(1)
	if chainLen <= 0 {
		return
	}
	target := uint64(chainLen)
	for {
		prev := perfCounters.reduceChainMaxLen.Load()
		if target <= prev {
			return
		}
		if perfCounters.reduceChainMaxLen.CompareAndSwap(prev, target) {
			return
		}
	}
}

func perfRecordReduceChainBreakMulti() {
	perfCounters.reduceChainBreakMulti.Add(1)
}

func perfRecordReduceChainBreakShift() {
	perfCounters.reduceChainBreakShift.Add(1)
}

func perfRecordReduceChainBreakAccept() {
	perfCounters.reduceChainBreakAccept.Add(1)
}

func perfRecordParentChildren(count int) {
	if count > 0 {
		perfCounters.parentChildPointers.Add(uint64(count))
	}
}

func perfRecordExtraNode() {
	perfCounters.extraNodes.Add(1)
}

func perfRecordErrorNode() {
	perfCounters.errorNodes.Add(1)
}

func perfMergeHistBin(n int) int {
	if n < 0 {
		return 0
	}
	if n >= perfMergeHistBins {
		return perfMergeHistBins - 1
	}
	return n
}

func perfForkHistBin(actions int) int {
	if actions <= 2 {
		return 0
	}
	if actions >= 9 {
		return perfForkHistBins - 1
	}
	return actions - 2
}

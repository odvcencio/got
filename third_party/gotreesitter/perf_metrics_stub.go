//go:build !perf

package gotreesitter

const perfCountersEnabled = false

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
	MergeStacksInHist      [maxGLRStacks + 2]uint64
	MergeAliveHist         [maxGLRStacks + 2]uint64
	MergeOutHist           [maxGLRStacks + 2]uint64
	ForkActionsHist        [8]uint64
}

func ResetPerfCounters()                 {}
func PerfCountersSnapshot() PerfCounters { return PerfCounters{} }

func perfRecordMergeCall(int)                  {}
func perfRecordMergeAlive(int, int)            {}
func perfRecordMergeOut(int)                   {}
func perfRecordMergeHashZero()                 {}
func perfRecordGlobalCapCull(int, int)         {}
func perfRecordMergePerKeyOverflow()           {}
func perfRecordMergeReplacement()              {}
func perfRecordStackEquivalentCall()           {}
func perfRecordStackEquivalentTrue()           {}
func perfRecordStackEquivalentHashMissSkip()   {}
func perfRecordStackCompare()                  {}
func perfRecordConflictRR()                    {}
func perfRecordConflictRS()                    {}
func perfRecordConflictOther()                 {}
func perfRecordFork(int, uint64)               {}
func perfRecordMaxConcurrentStacks(int)        {}
func perfRecordLexed(int, int)                 {}
func perfRecordReuseVisited()                  {}
func perfRecordReusePushed(int)                {}
func perfRecordReusePopped()                   {}
func perfRecordReuseCandidates(int)            {}
func perfRecordReuseSuccess()                  {}
func perfRecordReuseLeafSuccess()              {}
func perfRecordReuseNonLeafCheck()             {}
func perfRecordReuseNonLeafSuccess(uint32)     {}
func perfRecordReuseNonLeafNoGoto()            {}
func perfRecordReuseNonLeafNoGotoTerminal()    {}
func perfRecordReuseNonLeafNoGotoNonTerminal() {}
func perfRecordReuseNonLeafStateMiss()         {}
func perfRecordReuseNonLeafStateZero()         {}
func perfRecordReduceChainStep(int)            {}
func perfRecordReduceChainBreakMulti()         {}
func perfRecordReduceChainBreakShift()         {}
func perfRecordReduceChainBreakAccept()        {}
func perfRecordParentChildren(int)             {}
func perfRecordExtraNode()                     {}
func perfRecordErrorNode()                     {}

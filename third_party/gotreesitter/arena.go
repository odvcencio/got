package gotreesitter

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	// incrementalArenaSlab is sized for steady-state edits where only a small
	// frontier of nodes is rebuilt.
	incrementalArenaSlab = 16 * 1024
	// fullParseArenaSlab matches the current full-parse node footprint with
	// headroom, while remaining small enough to keep a warm pool.
	fullParseArenaSlab = 2 * 1024 * 1024
	minArenaNodeCap    = 64

	// Default capacities for slice backing storage used by reduce actions.
	// Full parses allocate many more parent-child edges than incremental edits.
	incrementalChildSliceCap = 2 * 1024
	fullChildSliceCap        = 64 * 1024
	incrementalFieldSliceCap = 2 * 1024
	fullFieldSliceCap        = 64 * 1024

	maxRetainedArenaFactor = 4
	// Full-parse node slabs are much larger; keep more headroom so capacity
	// growth does not thrash between parses.
	maxRetainedFullNodeArenaFactor = 16

	// Absolute node-cap retention ceilings to avoid repeated large reallocation
	// on warm edit/full-parse workloads.
	maxRetainedIncrementalNodeCap = 1 * 1024 * 1024
	maxRetainedFullNodeCap        = 2 * 1024 * 1024
)

type arenaClass uint8

const (
	arenaClassIncremental arenaClass = iota
	arenaClassFull
)

// nodeArena is a slab-backed allocator for Node structs.
// It uses ref counting so trees that borrow reused subtrees can keep arena
// memory alive safely until all dependent trees are released.
type nodeArena struct {
	class arenaClass
	nodes []Node
	used  int
	refs  atomic.Int32
	// skipChildClear allows reset() to skip child-slab pointer clearing when
	// a parse did not borrow any external nodes (full parse without reuse).
	skipChildClear bool

	nodeSlabs      []nodeSlab
	nodeSlabCursor int

	childSlabs      []childSliceSlab
	fieldSlabs      []fieldSliceSlab
	childSlabCursor int
	fieldSlabCursor int
}

type nodeSlab struct {
	data []Node
	used int
}

type childSliceSlab struct {
	data []*Node
	used int
}

type fieldSliceSlab struct {
	data []FieldID
	used int
}

var (
	incrementalArenaPool = nodeArenaPool{
		class:   arenaClassIncremental,
		maxSize: 8,
	}
	fullArenaPool = nodeArenaPool{
		class:   arenaClassFull,
		maxSize: 4,
	}
)

type nodeArenaPool struct {
	mu      sync.Mutex
	class   arenaClass
	maxSize int
	free      []*nodeArena
}

// ArenaProfile captures node arena allocation statistics.
// Enable with SetArenaProfileEnabled(true) and retrieve with GetArenaProfile().
type ArenaProfile struct {
	IncrementalAcquire uint64
	IncrementalNew     uint64
	FullAcquire        uint64
	FullNew            uint64
}

var (
	arenaProfileEnabled bool
	arenaProfileData    ArenaProfile
)

// EnableArenaProfile toggles arena pool counters.
// This debug hook is not concurrency-safe and is intended for single-threaded
// benchmark/profiling runs.
func EnableArenaProfile(enabled bool) {
	arenaProfileEnabled = enabled
}

// ResetArenaProfile resets arena pool counters.
// This debug hook is not concurrency-safe and is intended for single-threaded
// benchmark/profiling runs.
func ResetArenaProfile() {
	arenaProfileData = ArenaProfile{}
}

// ArenaProfileSnapshot returns current arena pool counters.
// This debug hook is not concurrency-safe and is intended for single-threaded
// benchmark/profiling runs.
func ArenaProfileSnapshot() ArenaProfile {
	return arenaProfileData
}

func (p *nodeArenaPool) acquire() *nodeArena {
	p.mu.Lock()
	n := len(p.free)
	if n == 0 {
		p.mu.Unlock()
		a := newNodeArena(p.class)
		if arenaProfileEnabled {
			switch p.class {
			case arenaClassIncremental:
				arenaProfileData.IncrementalAcquire++
				arenaProfileData.IncrementalNew++
			default:
				arenaProfileData.FullAcquire++
				arenaProfileData.FullNew++
			}
		}
		return a
	}
	a := p.free[n-1]
	p.free = p.free[:n-1]
	p.mu.Unlock()
	if arenaProfileEnabled {
		switch p.class {
		case arenaClassIncremental:
			arenaProfileData.IncrementalAcquire++
		default:
			arenaProfileData.FullAcquire++
		}
	}
	return a
}

func (p *nodeArenaPool) release(a *nodeArena) {
	if a == nil {
		return
	}
	p.mu.Lock()
	if len(p.free) < p.maxSize {
		p.free = append(p.free, a)
	}
	p.mu.Unlock()
}

func nodeCapacityForBytes(slabBytes int) int {
	nodeSize := int(unsafe.Sizeof(Node{}))
	if nodeSize <= 0 {
		return minArenaNodeCap
	}
	capacity := slabBytes / nodeSize
	if capacity < minArenaNodeCap {
		return minArenaNodeCap
	}
	return capacity
}

func newNodeArena(class arenaClass) *nodeArena {
	childCap := fullChildSliceCap
	fieldCap := fullFieldSliceCap
	if class == arenaClassIncremental {
		childCap = incrementalChildSliceCap
		fieldCap = incrementalFieldSliceCap
	}
	return &nodeArena{
		class:      class,
		nodes:      make([]Node, nodeCapacityForClass(class)),
		childSlabs: []childSliceSlab{{data: make([]*Node, childCap)}},
		fieldSlabs: []fieldSliceSlab{{data: make([]FieldID, fieldCap)}},
	}
}

func acquireNodeArena(class arenaClass) *nodeArena {
	var a *nodeArena
	switch class {
	case arenaClassIncremental:
		a = incrementalArenaPool.acquire()
	default:
		a = fullArenaPool.acquire()
	}
	a.refs.Store(1)
	return a
}

func (a *nodeArena) Retain() {
	if a == nil {
		return
	}
	a.refs.Add(1)
}

func (a *nodeArena) Release() {
	if a == nil {
		return
	}
	if a.refs.Add(-1) != 0 {
		return
	}
	a.reset()
	switch a.class {
	case arenaClassIncremental:
		incrementalArenaPool.release(a)
	default:
		fullArenaPool.release(a)
	}
}

func (a *nodeArena) reset() {
	primaryUsed := min(a.used, len(a.nodes))
	clear(a.nodes[:primaryUsed])
	a.used = 0
	for i := range a.nodeSlabs {
		slab := &a.nodeSlabs[i]
		clear(slab.data[:slab.used])
		slab.used = 0
	}
	if len(a.nodeSlabs) > 0 {
		retained := 0
		keep := 0
		limit := maxRetainedOverflowNodeCapacityForClass(a.class)
		for i := 0; i < len(a.nodeSlabs); i++ {
			capacity := len(a.nodeSlabs[i].data)
			if capacity <= 0 {
				break
			}
			if retained+capacity > limit {
				break
			}
			retained += capacity
			keep = i + 1
		}
		for i := keep; i < len(a.nodeSlabs); i++ {
			a.nodeSlabs[i] = nodeSlab{}
		}
		a.nodeSlabs = a.nodeSlabs[:keep]
	}
	a.nodeSlabCursor = 0

	for i := range a.childSlabs {
		slab := &a.childSlabs[i]
		if !a.skipChildClear {
			clear(slab.data[:slab.used])
		}
		slab.used = 0
	}
	a.skipChildClear = false
	for i := range a.fieldSlabs {
		a.fieldSlabs[i].used = 0
	}
	a.childSlabCursor = 0
	a.fieldSlabCursor = 0

	if len(a.nodes) > maxRetainedNodeCapacityForClass(a.class) {
		a.nodes = make([]Node, nodeCapacityForClass(a.class))
	}
	if len(a.childSlabs) == 0 {
		a.childSlabs = []childSliceSlab{{data: make([]*Node, defaultChildSliceCap(a.class))}}
	}
	if len(a.fieldSlabs) == 0 {
		a.fieldSlabs = []fieldSliceSlab{{data: make([]FieldID, defaultFieldSliceCap(a.class))}}
	}
}

func (a *nodeArena) allocNode() *Node {
	if a == nil {
		return &Node{}
	}
	return a.allocNodeFast()
}

func (a *nodeArena) allocNodeFast() *Node {
	if a.used < len(a.nodes) {
		n := &a.nodes[a.used]
		a.used++
		return n
	}
	return a.allocNodeSlow()
}

func (a *nodeArena) allocNodeSlow() *Node {
	if len(a.nodeSlabs) == 0 {
		capacity := max(nodeCapacityForClass(a.class), minArenaNodeCap)
		a.nodeSlabs = append(a.nodeSlabs, nodeSlab{data: make([]Node, capacity)})
		a.nodeSlabCursor = 0
	}
	if a.nodeSlabCursor < 0 || a.nodeSlabCursor >= len(a.nodeSlabs) {
		a.nodeSlabCursor = 0
	}
	for i := a.nodeSlabCursor; ; i++ {
		if i >= len(a.nodeSlabs) {
			lastCap := len(a.nodeSlabs[len(a.nodeSlabs)-1].data)
			capacity := max(lastCap*2, minArenaNodeCap)
			a.nodeSlabs = append(a.nodeSlabs, nodeSlab{data: make([]Node, capacity)})
		}

		slab := &a.nodeSlabs[i]
		if slab.used >= len(slab.data) {
			continue
		}
		idx := slab.used
		slab.used++
		a.nodeSlabCursor = i
		a.used++
		return &slab.data[idx]
	}
}

func (a *nodeArena) ensureNodeCapacity(min int) {
	if a == nil || min <= len(a.nodes) {
		return
	}
	if a.used > 0 {
		// Pre-sizing is only valid before the arena starts serving allocations.
		// Calling this after allocation begins is an internal usage bug.
		panic("ensureNodeCapacity called after arena allocations started")
	}
	newCap := max(len(a.nodes), minArenaNodeCap)
	for newCap < min {
		newCap *= 2
	}
	a.nodes = make([]Node, newCap)
	a.used = 0
	a.nodeSlabs = nil
	a.nodeSlabCursor = 0
}

func (a *nodeArena) allocNodeSlice(n int) []*Node {
	if n <= 0 {
		return nil
	}
	if a == nil {
		return make([]*Node, n)
	}

	if len(a.childSlabs) == 0 {
		a.childSlabs = append(a.childSlabs, childSliceSlab{data: make([]*Node, defaultChildSliceCap(a.class))})
		a.childSlabCursor = 0
	}
	if a.childSlabCursor < 0 || a.childSlabCursor >= len(a.childSlabs) {
		a.childSlabCursor = 0
	}

	for i := a.childSlabCursor; ; i++ {
		if i >= len(a.childSlabs) {
			capacity := max(defaultChildSliceCap(a.class), n)
			a.childSlabs = append(a.childSlabs, childSliceSlab{data: make([]*Node, capacity)})
		}

		slab := &a.childSlabs[i]
		if len(slab.data)-slab.used < n {
			continue
		}
		start := slab.used
		slab.used += n
		a.childSlabCursor = i
		return slab.data[start:slab.used]
	}
}

func (a *nodeArena) allocFieldIDSlice(n int) []FieldID {
	if n <= 0 {
		return nil
	}
	if a == nil {
		return make([]FieldID, n)
	}

	if len(a.fieldSlabs) == 0 {
		a.fieldSlabs = append(a.fieldSlabs, fieldSliceSlab{data: make([]FieldID, defaultFieldSliceCap(a.class))})
		a.fieldSlabCursor = 0
	}
	if a.fieldSlabCursor < 0 || a.fieldSlabCursor >= len(a.fieldSlabs) {
		a.fieldSlabCursor = 0
	}

	for i := a.fieldSlabCursor; ; i++ {
		if i >= len(a.fieldSlabs) {
			capacity := max(defaultFieldSliceCap(a.class), n)
			a.fieldSlabs = append(a.fieldSlabs, fieldSliceSlab{data: make([]FieldID, capacity)})
		}

		slab := &a.fieldSlabs[i]
		if len(slab.data)-slab.used < n {
			continue
		}
		start := slab.used
		slab.used += n
		a.fieldSlabCursor = i
		out := slab.data[start:slab.used]
		clear(out)
		return out
	}
}

func defaultChildSliceCap(class arenaClass) int {
	if class == arenaClassIncremental {
		return incrementalChildSliceCap
	}
	return fullChildSliceCap
}

func defaultFieldSliceCap(class arenaClass) int {
	if class == arenaClassIncremental {
		return incrementalFieldSliceCap
	}
	return fullFieldSliceCap
}

func nodeCapacityForClass(class arenaClass) int {
	if class == arenaClassIncremental {
		return nodeCapacityForBytes(incrementalArenaSlab)
	}
	return nodeCapacityForBytes(fullParseArenaSlab)
}

func maxRetainedNodeCapacityForClass(class arenaClass) int {
	factor := maxRetainedArenaFactor
	floor := maxRetainedIncrementalNodeCap
	if class == arenaClassFull {
		factor = maxRetainedFullNodeArenaFactor
		floor = maxRetainedFullNodeCap
	}
	return max(nodeCapacityForClass(class)*factor, floor)
}

func maxRetainedOverflowNodeCapacityForClass(class arenaClass) int {
	return max(maxRetainedNodeCapacityForClass(class)/2, nodeCapacityForClass(class))
}


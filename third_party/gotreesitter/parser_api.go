package gotreesitter

import (
	"errors"
	"fmt"
)

type parseConfig struct {
	oldTree     *Tree
	tokenSource TokenSource
	profiling   bool
}

// ParserLogType categorizes parser log messages.
type ParserLogType uint8

const (
	// ParserLogParse emits parser-loop lifecycle and control-flow logs.
	ParserLogParse ParserLogType = iota
	// ParserLogLex emits token-source and token-consumption logs.
	ParserLogLex
)

// ParserLogger receives parser debug logs when configured via SetLogger.
type ParserLogger func(kind ParserLogType, message string)

const (
	// Retry no-stacks-alive full parses with a wider GLR cap. Large real-world
	// files (for example this repo's parser.go) can legitimately need >8 stacks
	// at peak even when parse tables report narrower local conflict widths.
	fullParseRetryMaxGLRStacks = 32
	// Some ambiguity clusters need more survivors per merge bucket even after
	// the global GLR cap is widened. Only enable this on retries for parses
	// that already proved the default merge budget was insufficient.
	fullParseRetryMaxMergePerKey = 24
	// Retry node-limit full parses with a bounded larger node budget instead of
	// globally raising the default cap for every parse.
	fullParseRetryNodeLimitScale = 2
	// If the first widened retry still stops on node_limit, allow one more
	// bounded escalation. This only applies to parses that already proved the
	// initial retry made progress but still ran out of budget.
	fullParseRetrySecondaryNodeLimitScale = 3
	// Keep retry widening bounded to avoid runaway memory growth on very large
	// malformed inputs. Callers can still override via GOT_GLR_MAX_STACKS.
	fullParseRetryMaxSourceBytes = 1 << 20 // 1 MiB
)

type resettableTokenSource interface {
	Reset(source []byte)
}

func shouldRetryFullParse(tree *Tree, sourceLen int) bool {
	if tree == nil {
		return false
	}
	if tree.ParseStopReason() != ParseStopNoStacksAlive {
		return false
	}
	if sourceLen <= 0 {
		return false
	}
	return sourceLen <= fullParseRetryMaxSourceBytes
}

func shouldRetryAcceptedErrorParse(tree *Tree, sourceLen int, initialMaxStacks int) bool {
	if tree == nil {
		return false
	}
	if sourceLen <= 0 || sourceLen > fullParseRetryMaxSourceBytes {
		return false
	}
	root := tree.RootNode()
	if root == nil || !root.HasError() {
		return false
	}
	rt := tree.ParseRuntime()
	if rt.StopReason != ParseStopAccepted || rt.Truncated || rt.TokenSourceEOFEarly {
		return false
	}
	if initialMaxStacks <= 0 {
		initialMaxStacks = maxGLRStacks
	}
	return rt.MaxStacksSeen >= initialMaxStacks
}

func shouldRetryNodeLimitParse(tree *Tree, sourceLen int) bool {
	if tree == nil {
		return false
	}
	if sourceLen <= 0 || sourceLen > fullParseRetryMaxSourceBytes {
		return false
	}
	return tree.ParseStopReason() == ParseStopNodeLimit
}

func treeParseClean(tree *Tree) bool {
	if tree == nil {
		return false
	}
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return false
	}
	rt := tree.ParseRuntime()
	return rt.StopReason == ParseStopAccepted && !rt.Truncated && !rt.TokenSourceEOFEarly
}

func rootOrNil(tree *Tree) *Node {
	if tree == nil {
		return nil
	}
	return tree.RootNode()
}

func normalizeReturnedTree(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil {
		return
	}
	normalizeHTMLRecoveredNestedCustomTagRanges(root, source, lang)
}

func retryTreeEndByte(tree *Tree) uint32 {
	if tree == nil {
		return 0
	}
	if root := tree.RootNode(); root != nil {
		return root.EndByte()
	}
	return tree.ParseRuntime().RootEndByte
}

func retryTreeChildCount(tree *Tree) int {
	if tree == nil {
		return 0
	}
	if root := tree.RootNode(); root != nil {
		return root.ChildCount()
	}
	return 0
}

func retryTreeHasError(tree *Tree) bool {
	if tree == nil {
		return true
	}
	root := tree.RootNode()
	if root == nil {
		return true
	}
	return root.HasError()
}

func retryStopRank(rt ParseRuntime) int {
	switch rt.StopReason {
	case ParseStopAccepted:
		return 4
	case ParseStopTokenSourceEOF:
		return 3
	case ParseStopNoStacksAlive:
		return 2
	case ParseStopNodeLimit:
		return 1
	default:
		return 0
	}
}

func preferRetryTree(candidate, incumbent *Tree) bool {
	if candidate == nil {
		return false
	}
	if incumbent == nil {
		return true
	}
	if treeParseClean(candidate) {
		return !treeParseClean(incumbent)
	}
	if treeParseClean(incumbent) {
		return false
	}
	candEnd := retryTreeEndByte(candidate)
	incEnd := retryTreeEndByte(incumbent)
	if candEnd != incEnd {
		return candEnd > incEnd
	}
	candRT := candidate.ParseRuntime()
	incRT := incumbent.ParseRuntime()
	if candRT.Truncated != incRT.Truncated {
		return !candRT.Truncated
	}
	if candRT.TokenSourceEOFEarly != incRT.TokenSourceEOFEarly {
		return !candRT.TokenSourceEOFEarly
	}
	candErr := retryTreeHasError(candidate)
	incErr := retryTreeHasError(incumbent)
	if candErr != incErr {
		return !candErr
	}
	candStop := retryStopRank(candRT)
	incStop := retryStopRank(incRT)
	if candStop != incStop {
		return candStop > incStop
	}
	candChildren := retryTreeChildCount(candidate)
	incChildren := retryTreeChildCount(incumbent)
	if candChildren != incChildren {
		return candChildren < incChildren
	}
	return candRT.NodesAllocated < incRT.NodesAllocated
}

func scaledNodeLimit(limit, scale int) int {
	if limit <= 0 {
		return 0
	}
	if scale <= 1 {
		return limit
	}
	maxInt := int(^uint(0) >> 1)
	if limit > maxInt/scale {
		return maxInt
	}
	return limit * scale
}

func effectiveFullParseInitialMaxStacks(lang *Language, initialMaxStacks int) int {
	if initialMaxStacks <= 0 {
		initialMaxStacks = maxGLRStacks
	}
	if lang != nil && lang.Name == "bash" && initialMaxStacks < 256 {
		initialMaxStacks = 256
	}
	return initialMaxStacks
}

func fullParseRetryMaxStacksOverride(tree *Tree, sourceLen int, initialMaxStacks int) int {
	retryMaxStacks := fullParseRetryMaxGLRStacks
	if initialMaxStacks > retryMaxStacks {
		retryMaxStacks = initialMaxStacks * 2
	}
	if parseMaxGLRStacksValue() >= retryMaxStacks {
		return 0
	}
	if shouldRetryFullParse(tree, sourceLen) || shouldRetryAcceptedErrorParse(tree, sourceLen, initialMaxStacks) {
		return retryMaxStacks
	}
	return 0
}

func fullParseRetryNodeLimitOverride(tree *Tree, sourceLen int) int {
	if !shouldRetryNodeLimitParse(tree, sourceLen) {
		return 0
	}
	limit := tree.ParseRuntime().NodeLimit
	if limit <= 0 {
		limit = parseNodeLimit(sourceLen)
	}
	return scaledNodeLimit(limit, fullParseRetryNodeLimitScale)
}

func fullParseRetrySecondaryNodeLimitOverride(tree *Tree, sourceLen int) int {
	if tree == nil || sourceLen <= 0 || sourceLen > fullParseRetryMaxSourceBytes {
		return 0
	}
	rt := tree.ParseRuntime()
	if rt.StopReason != ParseStopNodeLimit {
		return 0
	}
	limit := rt.NodeLimit
	if limit <= 0 {
		return 0
	}
	return scaledNodeLimit(limit, fullParseRetrySecondaryNodeLimitScale)
}

func fullParseRetryMergePerKeyOverride(tree *Tree, sourceLen int, initialMaxStacks int) int {
	if tree == nil || sourceLen <= 0 || sourceLen > fullParseRetryMaxSourceBytes {
		return 0
	}
	if treeParseClean(tree) {
		return 0
	}
	rt := tree.ParseRuntime()
	if rt.TokenSourceEOFEarly {
		return 0
	}
	switch rt.StopReason {
	case ParseStopAccepted, ParseStopNoStacksAlive, ParseStopNodeLimit:
	default:
		return 0
	}
	if initialMaxStacks <= 0 {
		initialMaxStacks = maxGLRStacks
	}
	if rt.MaxStacksSeen < initialMaxStacks {
		return 0
	}
	return fullParseRetryMaxMergePerKey
}

func (p *Parser) retryFullParseWithDFA(source []byte, initialMaxStacks int, deterministicExternalConflicts bool, tree *Tree) *Tree {
	maxStacksOverride := fullParseRetryMaxStacksOverride(tree, len(source), initialMaxStacks)
	maxNodesOverride := fullParseRetryNodeLimitOverride(tree, len(source))
	if maxStacksOverride == 0 && maxNodesOverride == 0 {
		return tree
	}
	runRetry := func(maxMergePerKeyOverride int, maxNodes int) *Tree {
		retryLexer := NewLexer(p.language.LexStates, source)
		retryTS := acquireDFATokenSource(retryLexer, p.language, p.lookupActionIndex, p.hasKeywordState)
		return p.parseInternal(
			source,
			p.wrapIncludedRanges(retryTS),
			nil,
			nil,
			arenaClassFull,
			nil,
			maxStacksOverride,
			maxNodes,
			maxMergePerKeyOverride,
			deterministicExternalConflicts,
		)
	}
	bestTree := tree
	retryTree := runRetry(0, maxNodesOverride)
	if preferRetryTree(retryTree, bestTree) {
		bestTree = retryTree
	}
	nodeRetryTree := retryTree
	if extraNodeLimit := fullParseRetrySecondaryNodeLimitOverride(retryTree, len(source)); extraNodeLimit > 0 {
		nodeRetryTree = runRetry(0, extraNodeLimit)
		if preferRetryTree(nodeRetryTree, bestTree) {
			bestTree = nodeRetryTree
		}
	}
	if treeParseClean(bestTree) {
		return bestTree
	}
	maxMergePerKeyOverride := fullParseRetryMergePerKeyOverride(nodeRetryTree, len(source), initialMaxStacks)
	if maxMergePerKeyOverride == 0 {
		return bestTree
	}
	mergeRetryTree := runRetry(maxMergePerKeyOverride, maxNodesOverride)
	if preferRetryTree(mergeRetryTree, bestTree) {
		bestTree = mergeRetryTree
	}
	return bestTree
}

func (p *Parser) retryFullParseWithTokenSource(source []byte, ts TokenSource, initialMaxStacks int, deterministicExternalConflicts bool, tree *Tree) *Tree {
	maxStacksOverride := fullParseRetryMaxStacksOverride(tree, len(source), initialMaxStacks)
	maxNodesOverride := fullParseRetryNodeLimitOverride(tree, len(source))
	if maxStacksOverride == 0 && maxNodesOverride == 0 {
		return tree
	}
	resettable, ok := ts.(resettableTokenSource)
	if !ok {
		return tree
	}
	runRetry := func(maxMergePerKeyOverride int, maxNodes int) *Tree {
		resettable.Reset(source)
		return p.parseInternal(
			source,
			p.wrapIncludedRanges(ts),
			nil,
			nil,
			arenaClassFull,
			nil,
			maxStacksOverride,
			maxNodes,
			maxMergePerKeyOverride,
			deterministicExternalConflicts,
		)
	}
	bestTree := tree
	retryTree := runRetry(0, maxNodesOverride)
	if preferRetryTree(retryTree, bestTree) {
		bestTree = retryTree
	}
	nodeRetryTree := retryTree
	if extraNodeLimit := fullParseRetrySecondaryNodeLimitOverride(retryTree, len(source)); extraNodeLimit > 0 {
		nodeRetryTree = runRetry(0, extraNodeLimit)
		if preferRetryTree(nodeRetryTree, bestTree) {
			bestTree = nodeRetryTree
		}
	}
	if treeParseClean(bestTree) {
		return bestTree
	}
	maxMergePerKeyOverride := fullParseRetryMergePerKeyOverride(nodeRetryTree, len(source), initialMaxStacks)
	if maxMergePerKeyOverride == 0 {
		return bestTree
	}
	mergeRetryTree := runRetry(maxMergePerKeyOverride, maxNodesOverride)
	if preferRetryTree(mergeRetryTree, bestTree) {
		bestTree = mergeRetryTree
	}
	return bestTree
}

// ParseOption configures ParseWith behavior.
type ParseOption func(*parseConfig)

// WithOldTree enables incremental parsing against an edited prior tree.
func WithOldTree(oldTree *Tree) ParseOption {
	return func(c *parseConfig) {
		c.oldTree = oldTree
	}
}

// WithTokenSource provides a custom token source for parsing.
func WithTokenSource(ts TokenSource) ParseOption {
	return func(c *parseConfig) {
		c.tokenSource = ts
	}
}

// WithProfiling enables incremental parse attribution in ParseResult.Profile.
func WithProfiling() ParseOption {
	return func(c *parseConfig) {
		c.profiling = true
	}
}

// ParseResult is returned by ParseWith.
type ParseResult struct {
	Tree *Tree
	// Profile is populated only when ParseWith uses WithProfiling for
	// incremental parsing.
	Profile IncrementalParseProfile
	// ProfileAvailable reports whether Profile contains attribution data.
	ProfileAvailable bool
}

// Language returns the parser's configured language.
func (p *Parser) Language() *Language {
	if p == nil {
		return nil
	}
	return p.language
}

// SetGLRTrace enables verbose GLR stack tracing to stdout (debug only).
func (p *Parser) SetGLRTrace(enabled bool) {
	if p == nil {
		return
	}
	p.glrTrace = enabled
}

// SetLogger installs a parser debug logger. Pass nil to disable logging.
func (p *Parser) SetLogger(logger ParserLogger) {
	if p == nil {
		return
	}
	p.logger = logger
}

// Logger returns the currently configured parser debug logger.
func (p *Parser) Logger() ParserLogger {
	if p == nil {
		return nil
	}
	return p.logger
}

// SetTimeoutMicros configures a per-parse timeout in microseconds.
// A value of 0 disables timeout checks.
func (p *Parser) SetTimeoutMicros(timeoutMicros uint64) {
	if p == nil {
		return
	}
	p.timeoutMicros = timeoutMicros
}

// TimeoutMicros returns the parser timeout in microseconds.
func (p *Parser) TimeoutMicros() uint64 {
	if p == nil {
		return 0
	}
	return p.timeoutMicros
}

// SetCancellationFlag configures a caller-owned cancellation flag.
// Parsing stops when the pointed value becomes non-zero.
func (p *Parser) SetCancellationFlag(flag *uint32) {
	if p == nil {
		return
	}
	p.cancellationFlag = flag
}

// CancellationFlag returns the parser's current cancellation flag pointer.
func (p *Parser) CancellationFlag() *uint32 {
	if p == nil {
		return nil
	}
	return p.cancellationFlag
}

// SetIncludedRanges configures parser include ranges.
// Tokens outside these ranges are skipped.
func (p *Parser) SetIncludedRanges(ranges []Range) {
	if p == nil {
		return
	}
	p.included = normalizeIncludedRanges(ranges)
}

// IncludedRanges returns a copy of the configured include ranges.
func (p *Parser) IncludedRanges() []Range {
	if p == nil || len(p.included) == 0 {
		return nil
	}
	out := make([]Range, len(p.included))
	copy(out, p.included)
	return out
}

func (p *Parser) wrapIncludedRanges(ts TokenSource) TokenSource {
	if p == nil || len(p.included) == 0 || ts == nil {
		return ts
	}
	return newIncludedRangeTokenSource(ts, p.included)
}

// TokenSource provides tokens to the parser. This interface abstracts over
// different lexer implementations: the built-in DFA lexer (for hand-built
// grammars) or custom bridges like GoTokenSource (for real grammars where
// we can't extract the C lexer DFA).
type TokenSource interface {
	// Next returns the next token. It should skip whitespace and comments
	// as appropriate for the language. Returns a zero-Symbol token at EOF.
	Next() Token
}

// ByteSkippableTokenSource can jump to a byte offset and return the first
// token at or after that position.
type ByteSkippableTokenSource interface {
	TokenSource
	SkipToByte(offset uint32) Token
}

// PointSkippableTokenSource extends ByteSkippableTokenSource with a hint-based
// skip that avoids recomputing row/column from byte offset. During incremental
// parsing the reused node already carries its endpoint, so passing it directly
// eliminates the O(n) offset-to-point scan.
type PointSkippableTokenSource interface {
	ByteSkippableTokenSource
	SkipToByteWithPoint(offset uint32, pt Point) Token
}

// IncrementalReuseTokenSource is an opt-in marker for custom token sources
// that are safe for incremental subtree reuse. Implementations must provide
// stable token boundaries across edits and support deterministic SkipToByte*
// behavior so reused-tree fast-forwarding remains correct.
type IncrementalReuseTokenSource interface {
	TokenSource
	SupportsIncrementalReuse() bool
}

type parserStateTokenSource interface {
	SetParserState(state StateID)
	// SetGLRStates provides all active GLR stack states so the token source
	// can compute valid external symbols as the union across all stacks.
	// This is critical for grammars with external scanners and GLR conflicts.
	SetGLRStates(states []StateID)
}

// stackEntry is a single entry on the parser's LR stack, pairing a parser
// state with the syntax tree node that was shifted or reduced into that state.
type stackEntry struct {
	state StateID
	node  *Node
}

// errorSymbol is the well-known symbol ID used for error nodes.
const errorSymbol = Symbol(65535)

// Parse tokenizes and parses source using the built-in DFA lexer, returning
// a syntax tree. This works for hand-built grammars that provide LexStates.
// For real grammars that need a custom lexer, use ParseWithTokenSource.
// If the input is empty, it returns a tree with a nil root and no error.
func (p *Parser) Parse(source []byte) (*Tree, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, err
	}
	if err := p.checkDFALexer(); err != nil {
		return nil, err
	}
	lexer := NewLexer(p.language.LexStates, source)
	ts := acquireDFATokenSource(lexer, p.language, p.lookupActionIndex, p.hasKeywordState)
	deterministicExternalConflicts := p.language != nil &&
		p.language.ExternalScanner != nil &&
		(p.language.Name == "yaml" || p.language.Name == "scala")
	initialMaxStacks := effectiveFullParseInitialMaxStacks(p.language, parseMaxGLRStacksValue())
	if p.maxConflictWidth > initialMaxStacks {
		initialMaxStacks = p.maxConflictWidth
	}
	tree := p.parseInternal(source, p.wrapIncludedRanges(ts), nil, nil, arenaClassFull, nil, 0, 0, 0, deterministicExternalConflicts)
	tree = p.retryFullParseWithDFA(source, initialMaxStacks, deterministicExternalConflicts, tree)
	if p.language.ExternalScanner != nil &&
		tree != nil {
		tree = p.retryFullParseWithDFA(source, initialMaxStacks, deterministicExternalConflicts, tree)
	}
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, nil
}

// ParseWithTokenSource parses source using a custom token source.
// This is used for real grammars where the lexer DFA isn't available
// as data tables (e.g., Go grammar using go/scanner as a bridge).
func (p *Parser) ParseWithTokenSource(source []byte, ts TokenSource) (*Tree, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, err
	}
	deterministicExternalConflicts := p.language != nil &&
		p.language.ExternalScanner != nil &&
		(p.language.Name == "yaml" || p.language.Name == "scala")
	initialMaxStacks := effectiveFullParseInitialMaxStacks(p.language, parseMaxGLRStacksValue())
	if p.maxConflictWidth > initialMaxStacks {
		initialMaxStacks = p.maxConflictWidth
	}
	tree := p.parseInternal(source, p.wrapIncludedRanges(ts), nil, nil, arenaClassFull, nil, 0, 0, 0, deterministicExternalConflicts)
	tree = p.retryFullParseWithTokenSource(source, ts, initialMaxStacks, deterministicExternalConflicts, tree)
	if p.language.ExternalScanner != nil &&
		tree != nil {
		tree = p.retryFullParseWithTokenSource(source, ts, initialMaxStacks, deterministicExternalConflicts, tree)
	}
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, nil
}

// ParseIncremental re-parses source after edits were applied to oldTree.
// It reuses unchanged subtrees from the old tree for better performance.
// Call oldTree.Edit() for each edit before calling this method.
func (p *Parser) ParseIncremental(source []byte, oldTree *Tree) (*Tree, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, err
	}
	if canReuseUnchangedTree(source, oldTree, p.language) {
		return oldTree, nil
	}
	if err := p.checkDFALexer(); err != nil {
		return nil, err
	}
	lexer := NewLexer(p.language.LexStates, source)
	ts := acquireDFATokenSource(lexer, p.language, p.lookupActionIndex, p.hasKeywordState)
	tree := p.parseIncrementalInternal(source, oldTree, p.wrapIncludedRanges(ts), nil)
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, nil
}

// ParseIncrementalWithTokenSource is like ParseIncremental but uses a custom
// token source.
func (p *Parser) ParseIncrementalWithTokenSource(source []byte, oldTree *Tree, ts TokenSource) (*Tree, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, err
	}
	if canReuseUnchangedTree(source, oldTree, p.language) {
		return oldTree, nil
	}
	tree := p.parseIncrementalInternal(source, oldTree, p.wrapIncludedRanges(ts), nil)
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, nil
}

// ParseIncrementalProfiled is like ParseIncremental and also returns runtime
// attribution for incremental reuse work vs parse/rebuild work.
func (p *Parser) ParseIncrementalProfiled(source []byte, oldTree *Tree) (*Tree, IncrementalParseProfile, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, IncrementalParseProfile{}, err
	}
	if canReuseUnchangedTree(source, oldTree, p.language) {
		return oldTree, IncrementalParseProfile{}, nil
	}
	if err := p.checkDFALexer(); err != nil {
		return nil, IncrementalParseProfile{}, err
	}
	lexer := NewLexer(p.language.LexStates, source)
	ts := acquireDFATokenSource(lexer, p.language, p.lookupActionIndex, p.hasKeywordState)
	timing := &incrementalParseTiming{}
	tree := p.parseIncrementalInternal(source, oldTree, p.wrapIncludedRanges(ts), timing)
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, timing.toProfile(), nil
}

// ParseIncrementalWithTokenSourceProfiled is like ParseIncrementalWithTokenSource
// and also returns runtime attribution for incremental reuse work vs parse/rebuild work.
func (p *Parser) ParseIncrementalWithTokenSourceProfiled(source []byte, oldTree *Tree, ts TokenSource) (*Tree, IncrementalParseProfile, error) {
	if err := p.checkLanguageCompatible(); err != nil {
		return nil, IncrementalParseProfile{}, err
	}
	if canReuseUnchangedTree(source, oldTree, p.language) {
		return oldTree, IncrementalParseProfile{}, nil
	}
	timing := &incrementalParseTiming{}
	tree := p.parseIncrementalInternal(source, oldTree, p.wrapIncludedRanges(ts), timing)
	normalizeReturnedTree(rootOrNil(tree), source, p.language)
	return tree, timing.toProfile(), nil
}

// ParseWith parses source using option-based configuration.
func (p *Parser) ParseWith(source []byte, opts ...ParseOption) (ParseResult, error) {
	var cfg parseConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.profiling {
		if cfg.oldTree != nil {
			if cfg.tokenSource != nil {
				tree, profile, err := p.ParseIncrementalWithTokenSourceProfiled(source, cfg.oldTree, cfg.tokenSource)
				return ParseResult{Tree: tree, Profile: profile, ProfileAvailable: true}, err
			}
			tree, profile, err := p.ParseIncrementalProfiled(source, cfg.oldTree)
			return ParseResult{Tree: tree, Profile: profile, ProfileAvailable: true}, err
		}
		// Full parses do not currently expose attribution data.
		if cfg.tokenSource != nil {
			tree, err := p.ParseWithTokenSource(source, cfg.tokenSource)
			return ParseResult{Tree: tree, ProfileAvailable: false}, err
		}
		tree, err := p.Parse(source)
		return ParseResult{Tree: tree, ProfileAvailable: false}, err
	}

	if cfg.oldTree != nil {
		if cfg.tokenSource != nil {
			tree, err := p.ParseIncrementalWithTokenSource(source, cfg.oldTree, cfg.tokenSource)
			return ParseResult{Tree: tree, ProfileAvailable: false}, err
		}
		tree, err := p.ParseIncremental(source, cfg.oldTree)
		return ParseResult{Tree: tree, ProfileAvailable: false}, err
	}

	if cfg.tokenSource != nil {
		tree, err := p.ParseWithTokenSource(source, cfg.tokenSource)
		return ParseResult{Tree: tree, ProfileAvailable: false}, err
	}
	tree, err := p.Parse(source)
	return ParseResult{Tree: tree, ProfileAvailable: false}, err
}

// ErrNoLanguage is returned when a Parser has no language configured.
var ErrNoLanguage = errors.New("parser has no language configured")

// checkLanguageCompatible returns an error if the parser's language is nil or
// incompatible with the runtime.
func (p *Parser) checkLanguageCompatible() error {
	if p.language == nil {
		return ErrNoLanguage
	}
	if !p.language.CompatibleWithRuntime() {
		return fmt.Errorf("language version %d incompatible with parser", p.language.LanguageVersion)
	}
	return nil
}

// checkDFALexer returns an error if the parser's language has no DFA lexer tables.
func (p *Parser) checkDFALexer() error {
	if p.language == nil || len(p.language.LexStates) == 0 {
		return fmt.Errorf("no DFA lexer available for language (use ParseWithTokenSource instead)")
	}
	return nil
}

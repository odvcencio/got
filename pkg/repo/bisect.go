package repo

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// BisectResult holds the outcome of a bisect step (start, good, bad, skip).
type BisectResult struct {
	Done      bool        // true if bisect is complete (found first bad commit)
	FirstBad  object.Hash // set when Done is true
	Current   object.Hash // the commit checked out for testing
	Remaining int         // estimated remaining commits to test
	Steps     int         // estimated remaining steps (log2 of remaining)
	Message   string      // first line of current commit message
}

// BisectStart initializes a bisect session. It validates that both bad and good
// are reachable commits, saves state in .graft/bisect/, and checks out the
// midpoint commit for testing. Returns an error if already bisecting.
func (r *Repo) BisectStart(bad, good object.Hash) (*BisectResult, error) {
	if r.IsBisecting() {
		return nil, fmt.Errorf("bisect: already bisecting (use bisect reset first)")
	}

	// Validate that both hashes point to readable commits.
	if _, err := r.Store.ReadCommit(bad); err != nil {
		return nil, fmt.Errorf("bisect start: bad commit %s: %w", bad, err)
	}
	if _, err := r.Store.ReadCommit(good); err != nil {
		return nil, fmt.Errorf("bisect start: good commit %s: %w", good, err)
	}

	// Record the current HEAD ref so we can restore it on reset.
	startRef, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("bisect start: read HEAD: %w", err)
	}

	// Write initial state.
	if err := r.bisectWriteState(bad, []object.Hash{good}, startRef); err != nil {
		return nil, fmt.Errorf("bisect start: %w", err)
	}

	// Write initial log entries.
	if err := r.bisectAppendLog(fmt.Sprintf("# bad: %s", bad)); err != nil {
		return nil, fmt.Errorf("bisect start: %w", err)
	}
	if err := r.bisectAppendLog(fmt.Sprintf("# good: %s", good)); err != nil {
		return nil, fmt.Errorf("bisect start: %w", err)
	}

	// Find midpoint and checkout.
	return r.bisectAdvance(bad, []object.Hash{good})
}

// BisectGood marks the current HEAD as good, narrows the search, and checks
// out the next midpoint. If the first bad commit is found, Done is true.
func (r *Repo) BisectGood() (*BisectResult, error) {
	if !r.IsBisecting() {
		return nil, fmt.Errorf("bisect good: not bisecting")
	}

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("bisect good: resolve HEAD: %w", err)
	}

	bad, err := r.bisectReadBad()
	if err != nil {
		return nil, fmt.Errorf("bisect good: %w", err)
	}
	goods, err := r.bisectReadGoods()
	if err != nil {
		return nil, fmt.Errorf("bisect good: %w", err)
	}

	// Add current HEAD to the good set.
	goods = append(goods, head)

	// Persist updated goods list.
	if err := r.bisectWriteGoods(goods); err != nil {
		return nil, fmt.Errorf("bisect good: %w", err)
	}
	if err := r.bisectAppendLog(fmt.Sprintf("# good: %s", head)); err != nil {
		return nil, fmt.Errorf("bisect good: %w", err)
	}

	return r.bisectAdvance(bad, goods)
}

// BisectBad marks the current HEAD as bad, narrows the search, and checks
// out the next midpoint.
func (r *Repo) BisectBad() (*BisectResult, error) {
	if !r.IsBisecting() {
		return nil, fmt.Errorf("bisect bad: not bisecting")
	}

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("bisect bad: resolve HEAD: %w", err)
	}

	goods, err := r.bisectReadGoods()
	if err != nil {
		return nil, fmt.Errorf("bisect bad: %w", err)
	}

	// The current HEAD becomes the new bad commit.
	bad := head

	// Persist updated bad.
	if err := r.bisectWriteBad(bad); err != nil {
		return nil, fmt.Errorf("bisect bad: %w", err)
	}
	if err := r.bisectAppendLog(fmt.Sprintf("# bad: %s", head)); err != nil {
		return nil, fmt.Errorf("bisect bad: %w", err)
	}

	return r.bisectAdvance(bad, goods)
}

// BisectSkip skips the current commit (can't test) and tries another nearby
// candidate.
func (r *Repo) BisectSkip() (*BisectResult, error) {
	if !r.IsBisecting() {
		return nil, fmt.Errorf("bisect skip: not bisecting")
	}

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("bisect skip: resolve HEAD: %w", err)
	}

	bad, err := r.bisectReadBad()
	if err != nil {
		return nil, fmt.Errorf("bisect skip: %w", err)
	}
	goods, err := r.bisectReadGoods()
	if err != nil {
		return nil, fmt.Errorf("bisect skip: %w", err)
	}

	if err := r.bisectAppendLog(fmt.Sprintf("# skip: %s", head)); err != nil {
		return nil, fmt.Errorf("bisect skip: %w", err)
	}

	// Find candidates excluding the current head, then pick an alternative.
	candidates, err := r.bisectCandidates(bad, goods)
	if err != nil {
		return nil, fmt.Errorf("bisect skip: %w", err)
	}

	// Remove the current head from the candidate list.
	var filtered []object.Hash
	for _, c := range candidates {
		if c != head {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("bisect skip: no other candidates to test")
	}

	// Pick a candidate near the midpoint of the filtered list.
	mid := len(filtered) / 2
	next := filtered[mid]
	remaining := len(filtered)
	steps := bisectSteps(remaining)

	// Checkout the candidate.
	if err := r.Checkout(string(next)); err != nil {
		return nil, fmt.Errorf("bisect skip: checkout %s: %w", next, err)
	}

	msg, _ := r.bisectCommitMessage(next)

	return &BisectResult{
		Done:      false,
		Current:   next,
		Remaining: remaining,
		Steps:     steps,
		Message:   msg,
	}, nil
}

// BisectReset ends the bisect session, restores the original HEAD (from
// start-ref), and deletes the .graft/bisect/ directory.
func (r *Repo) BisectReset() error {
	if !r.IsBisecting() {
		return fmt.Errorf("bisect reset: not bisecting")
	}

	// Read start-ref to restore HEAD.
	startRef, err := r.bisectReadStartRef()
	if err != nil {
		return fmt.Errorf("bisect reset: %w", err)
	}

	// Restore HEAD. startRef is either "refs/heads/<branch>" or a raw hash.
	if strings.HasPrefix(startRef, "refs/heads/") {
		branchName := strings.TrimPrefix(startRef, "refs/heads/")
		if err := r.Checkout(branchName); err != nil {
			return fmt.Errorf("bisect reset: checkout %s: %w", branchName, err)
		}
	} else {
		// Detached HEAD case: checkout the raw hash.
		if err := r.Checkout(string(startRef)); err != nil {
			return fmt.Errorf("bisect reset: checkout %s: %w", startRef, err)
		}
	}

	// Remove bisect state directory.
	if err := os.RemoveAll(r.bisectDir()); err != nil {
		return fmt.Errorf("bisect reset: remove state: %w", err)
	}

	return nil
}

// BisectLog returns the bisect log lines from the log file.
func (r *Repo) BisectLog() ([]string, error) {
	if !r.IsBisecting() {
		return nil, fmt.Errorf("bisect log: not bisecting")
	}

	data, err := os.ReadFile(filepath.Join(r.bisectDir(), "log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("bisect log: %w", err)
	}

	content := strings.TrimRight(string(data), "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

// IsBisecting checks if a bisect session is currently in progress.
func (r *Repo) IsBisecting() bool {
	info, err := os.Stat(r.bisectDir())
	return err == nil && info.IsDir()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// bisectDir returns the path to the .graft/bisect/ directory.
func (r *Repo) bisectDir() string {
	return filepath.Join(r.GraftDir, "bisect")
}

// bisectWriteState writes all bisect state files to .graft/bisect/.
func (r *Repo) bisectWriteState(bad object.Hash, goods []object.Hash, startRef string) error {
	dir := r.bisectDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write bisect state: mkdir: %w", err)
	}

	// Write bad hash.
	if err := os.WriteFile(filepath.Join(dir, "bad"), []byte(string(bad)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write bisect state: bad: %w", err)
	}

	// Write good hashes (one per line).
	if err := r.bisectWriteGoods(goods); err != nil {
		return err
	}

	// Write start-ref.
	if err := os.WriteFile(filepath.Join(dir, "start-ref"), []byte(startRef+"\n"), 0o644); err != nil {
		return fmt.Errorf("write bisect state: start-ref: %w", err)
	}

	return nil
}

// bisectWriteBad writes the bad hash to the state file.
func (r *Repo) bisectWriteBad(bad object.Hash) error {
	return os.WriteFile(filepath.Join(r.bisectDir(), "bad"), []byte(string(bad)+"\n"), 0o644)
}

// bisectWriteGoods writes the list of good hashes to the state file.
func (r *Repo) bisectWriteGoods(goods []object.Hash) error {
	var lines []string
	for _, g := range goods {
		lines = append(lines, string(g))
	}
	data := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(filepath.Join(r.bisectDir(), "good"), []byte(data), 0o644)
}

// bisectReadBad reads the bad commit hash from state.
func (r *Repo) bisectReadBad() (object.Hash, error) {
	data, err := os.ReadFile(filepath.Join(r.bisectDir(), "bad"))
	if err != nil {
		return "", fmt.Errorf("read bisect bad: %w", err)
	}
	return object.Hash(strings.TrimSpace(string(data))), nil
}

// bisectReadGoods reads the list of good commit hashes from state.
func (r *Repo) bisectReadGoods() ([]object.Hash, error) {
	data, err := os.ReadFile(filepath.Join(r.bisectDir(), "good"))
	if err != nil {
		return nil, fmt.Errorf("read bisect goods: %w", err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, nil
	}
	lines := strings.Split(content, "\n")
	goods := make([]object.Hash, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			goods = append(goods, object.Hash(line))
		}
	}
	return goods, nil
}

// bisectReadStartRef reads the original HEAD ref from state.
func (r *Repo) bisectReadStartRef() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.bisectDir(), "start-ref"))
	if err != nil {
		return "", fmt.Errorf("read bisect start-ref: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// bisectAppendLog appends a line to the bisect log file.
func (r *Repo) bisectAppendLog(line string) error {
	logPath := filepath.Join(r.bisectDir(), "log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("append bisect log: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("append bisect log: %w", err)
	}
	return nil
}

// bisectWriteExpectedSteps writes the estimated remaining steps to state.
func (r *Repo) bisectWriteExpectedSteps(steps int) error {
	data := fmt.Sprintf("%d\n", steps)
	return os.WriteFile(filepath.Join(r.bisectDir(), "expected-steps"), []byte(data), 0o644)
}

// bisectAdvance finds the midpoint between bad and goods, checks it out, and
// returns a BisectResult. If only one candidate remains, bisect is done.
func (r *Repo) bisectAdvance(bad object.Hash, goods []object.Hash) (*BisectResult, error) {
	midpoint, remaining, err := r.bisectFindMidpoint(bad, goods)
	if err != nil {
		return nil, fmt.Errorf("bisect advance: %w", err)
	}

	steps := bisectSteps(remaining)

	// Write expected steps.
	if err := r.bisectWriteExpectedSteps(steps); err != nil {
		return nil, fmt.Errorf("bisect advance: %w", err)
	}

	// If no candidates remain, the bad commit is the first bad commit.
	if remaining == 0 {
		msg, _ := r.bisectCommitMessage(bad)
		return &BisectResult{
			Done:      true,
			FirstBad:  bad,
			Current:   bad,
			Remaining: 0,
			Steps:     0,
			Message:   msg,
		}, nil
	}

	// Checkout the midpoint.
	if err := r.Checkout(string(midpoint)); err != nil {
		return nil, fmt.Errorf("bisect advance: checkout %s: %w", midpoint, err)
	}

	msg, _ := r.bisectCommitMessage(midpoint)

	return &BisectResult{
		Done:      false,
		Current:   midpoint,
		Remaining: remaining,
		Steps:     steps,
		Message:   msg,
	}, nil
}

// bisectCandidates returns the set of commits reachable from bad but not from
// any good, sorted by topological distance from bad (oldest first).
func (r *Repo) bisectCandidates(bad object.Hash, goods []object.Hash) ([]object.Hash, error) {
	// Walk backwards from bad collecting all reachable commits.
	reachableFromBad := make(map[object.Hash]int) // hash -> distance from bad
	if err := r.bisectWalkAncestors(bad, reachableFromBad, 0); err != nil {
		return nil, fmt.Errorf("walk from bad: %w", err)
	}

	// Walk backwards from each good and collect reachable commits.
	reachableFromGood := make(map[object.Hash]bool)
	for _, g := range goods {
		if err := r.bisectMarkGoodAncestors(g, reachableFromGood); err != nil {
			return nil, fmt.Errorf("walk from good %s: %w", g, err)
		}
	}

	// Candidates = reachable from bad minus reachable from any good, also
	// excluding the bad commit itself (it's already known to be bad).
	var candidates []candidate
	for h, dist := range reachableFromBad {
		if h == bad {
			continue
		}
		if !reachableFromGood[h] {
			candidates = append(candidates, candidate{h, dist})
		}
	}

	// Sort by distance from bad (highest distance = oldest first).
	// This puts the oldest commits first and the bad commit (distance 0) last.
	sortCandidates(candidates)

	result := make([]object.Hash, len(candidates))
	for i, c := range candidates {
		result[i] = c.hash
	}
	return result, nil
}

// bisectFindMidpoint finds the commit roughly halfway between bad and goods.
// Returns the midpoint hash and the number of remaining candidates.
func (r *Repo) bisectFindMidpoint(bad object.Hash, goods []object.Hash) (object.Hash, int, error) {
	candidates, err := r.bisectCandidates(bad, goods)
	if err != nil {
		return "", 0, err
	}

	if len(candidates) == 0 {
		return bad, 0, nil
	}

	if len(candidates) == 1 {
		return candidates[0], 1, nil
	}

	// Pick the midpoint (middle of the sorted candidate list).
	mid := len(candidates) / 2
	return candidates[mid], len(candidates), nil
}

// bisectWalkAncestors performs a BFS from the given commit, recording each
// commit's distance from the starting point.
func (r *Repo) bisectWalkAncestors(start object.Hash, visited map[object.Hash]int, startDist int) error {
	type item struct {
		hash object.Hash
		dist int
	}
	queue := []item{{start, startDist}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if _, seen := visited[cur.hash]; seen {
			continue
		}
		visited[cur.hash] = cur.dist

		commit, err := r.Store.ReadCommit(cur.hash)
		if err != nil {
			return fmt.Errorf("read commit %s: %w", cur.hash, err)
		}

		for _, parent := range commit.Parents {
			if _, seen := visited[parent]; !seen {
				queue = append(queue, item{parent, cur.dist + 1})
			}
		}
	}
	return nil
}

// bisectMarkGoodAncestors walks backwards from a good commit marking all
// ancestors as reachable from good.
func (r *Repo) bisectMarkGoodAncestors(start object.Hash, visited map[object.Hash]bool) error {
	queue := []object.Hash{start}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur] {
			continue
		}
		visited[cur] = true

		commit, err := r.Store.ReadCommit(cur)
		if err != nil {
			return fmt.Errorf("read commit %s: %w", cur, err)
		}

		for _, parent := range commit.Parents {
			if !visited[parent] {
				queue = append(queue, parent)
			}
		}
	}
	return nil
}

// bisectCommitMessage reads the first line of a commit's message.
func (r *Repo) bisectCommitMessage(h object.Hash) (string, error) {
	commit, err := r.Store.ReadCommit(h)
	if err != nil {
		return "", err
	}
	msg := commit.Message
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		msg = msg[:idx]
	}
	return msg, nil
}

// bisectSteps computes the estimated number of remaining bisect steps from the
// number of remaining candidates.
func bisectSteps(remaining int) int {
	if remaining <= 1 {
		return 0
	}
	return int(math.Ceil(math.Log2(float64(remaining))))
}

// sortCandidates sorts by distance descending (oldest/farthest first).
type candidate struct {
	hash     object.Hash
	distance int
}

func sortCandidates(candidates []candidate) {
	// Simple insertion sort — candidate lists are typically small.
	for i := 1; i < len(candidates); i++ {
		key := candidates[i]
		j := i - 1
		for j >= 0 && candidates[j].distance < key.distance {
			candidates[j+1] = candidates[j]
			j--
		}
		candidates[j+1] = key
	}
}

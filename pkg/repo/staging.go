package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/object"

	"github.com/odvcencio/gotreesitter/grammars"
)

// StagingEntry records the staged state of a single file.
type StagingEntry struct {
	Path           string      `json:"path"`
	BlobHash       object.Hash `json:"blob_hash"`
	EntityListHash object.Hash `json:"entity_list_hash,omitempty"`
	Mode           string      `json:"mode,omitempty"`
	Conflict       bool        `json:"conflict,omitempty"`
	BaseBlobHash   object.Hash `json:"base_blob_hash,omitempty"`
	OursBlobHash   object.Hash `json:"ours_blob_hash,omitempty"`
	TheirsBlobHash object.Hash `json:"theirs_blob_hash,omitempty"`
	ModTime        int64       `json:"mod_time"`
	Size           int64       `json:"size"`
	HasChangeTime  bool        `json:"has_change_time,omitempty"`
	ChangeTimeNano int64       `json:"change_time_nano,omitempty"`
	HasFileID      bool        `json:"has_file_id,omitempty"`
	Device         uint64      `json:"device,omitempty"`
	Inode          uint64      `json:"inode,omitempty"`
}

// Staging holds the full staging area (index) for a Graft repository.
type Staging struct {
	Entries map[string]*StagingEntry `json:"entries"`
}

const (
	AddProgressPhaseScanStart      = "scan_start"
	AddProgressPhaseScanComplete   = "scan_complete"
	AddProgressPhaseStageFile      = "stage_file"
	AddProgressPhaseEntityStart    = "entity_start"
	AddProgressPhaseEntityFile     = "entity_file"
	AddProgressPhaseEntityComplete = "entity_complete"
	AddProgressPhaseWriteIndex     = "write_index"
)

type AddProgress struct {
	Phase   string
	Path    string
	Current int
	Total   int
}

type AddProgressFunc func(AddProgress)

type preparedAddEntry struct {
	entry   *StagingEntry
	content []byte // retained for Phase 2 entity extraction
	err     error
}

// indexPath returns the filesystem path to the staging index file.
func (r *Repo) indexPath() string {
	return filepath.Join(r.GraftDir, "index")
}

// ReadStaging loads the staging area from .graft/index. If the file does not
// exist, an empty Staging is returned (no error).
func (r *Repo) ReadStaging() (*Staging, error) {
	data, err := os.ReadFile(r.indexPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Staging{Entries: make(map[string]*StagingEntry)}, nil
		}
		return nil, fmt.Errorf("read staging: %w", err)
	}

	var stg Staging
	if err := json.Unmarshal(data, &stg); err != nil {
		return nil, fmt.Errorf("read staging: unmarshal: %w", err)
	}
	if stg.Entries == nil {
		stg.Entries = make(map[string]*StagingEntry)
	}
	return &stg, nil
}

// WriteStaging atomically writes the staging area to .graft/index.
func (r *Repo) WriteStaging(s *Staging) error {
	return r.writeStaging(s, true)
}

func (r *Repo) writeStaging(s *Staging, invalidateStatusCache bool) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("write staging: marshal: %w", err)
	}

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(r.GraftDir, ".index-tmp-*")
	if err != nil {
		return fmt.Errorf("write staging: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write staging: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write staging: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write staging: close: %w", err)
	}

	if err := os.Rename(tmpName, r.indexPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write staging: rename: %w", err)
	}

	if invalidateStatusCache {
		r.invalidateStatusCache()
	}

	return nil
}

// AddOptions controls optional behavior for the Add operation.
type AddOptions struct {
	// SkipEntities disables entity extraction during staging. Blobs are
	// still written; entity lists can be computed later.
	SkipEntities bool

	// ForceEntities bypasses the data format size denylist and always
	// extracts entities, even for large JSON/YAML/TOML files.
	ForceEntities bool
}

// Add stages the given file paths. Each path is resolved relative to the
// repo root. For each file:
//  1. The raw content is written as a blob to the object store.
//  2. Entity extraction is attempted. If successful, each entity is written
//     as an EntityObj, and an EntityListObj referencing them is also stored.
//  3. A StagingEntry is created/updated with the resulting hashes and file
//     metadata, and the staging area is flushed to disk.
func (r *Repo) Add(paths []string) error {
	return r.add(paths, nil, AddOptions{})
}

// AddWithProgress stages files while emitting coarse-grained progress events.
func (r *Repo) AddWithProgress(paths []string, progress AddProgressFunc) error {
	return r.add(paths, progress, AddOptions{})
}

// AddWithOptions stages files with the given options and progress callback.
func (r *Repo) AddWithOptions(paths []string, progress AddProgressFunc, opts AddOptions) error {
	return r.add(paths, progress, opts)
}

// blobResult holds the output of Phase 1 (blob staging) for a single file.
type blobResult struct {
	relPath string
	entry   *StagingEntry
	content []byte // retained for Phase 2 entity extraction
}

// sourceBytesSemaphore limits aggregate in-flight source bytes during entity
// extraction to bound memory usage.
type sourceBytesSemaphore struct {
	ch     chan struct{}
	mu     sync.Mutex
	used   int64
	budget int64
}

func newSourceBytesSemaphore(budgetMB int) *sourceBytesSemaphore {
	if budgetMB <= 0 {
		budgetMB = 64
	}
	return &sourceBytesSemaphore{
		budget: int64(budgetMB) * 1024 * 1024,
		ch:     make(chan struct{}, 1),
	}
}

func (s *sourceBytesSemaphore) Acquire(ctx context.Context, n int64) error {
	for {
		s.mu.Lock()
		if s.used+n <= s.budget || s.used == 0 {
			s.used += n
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ch:
			// retry
		}
	}
}

func (s *sourceBytesSemaphore) Release(n int64) {
	s.mu.Lock()
	s.used -= n
	s.mu.Unlock()
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

// entityWorkerCount returns the number of concurrent entity extraction workers.
func entityWorkerCount() int {
	if v := os.Getenv("GRAFT_ENTITY_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 2
}

// entityMemoryBudgetMB returns the memory budget (in MB) for in-flight source
// bytes during entity extraction.
func entityMemoryBudgetMB() int {
	if v := os.Getenv("GRAFT_ENTITY_MEMORY_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 64
}

func (r *Repo) add(paths []string, progress AddProgressFunc, opts AddOptions) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	emitAddProgress(progress, AddProgress{Phase: AddProgressPhaseScanStart})
	toAdd, err := r.expandAddPaths(paths)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}
	if len(toAdd) == 0 {
		return fmt.Errorf("add: no files matched")
	}
	emitAddProgress(progress, AddProgress{
		Phase: AddProgressPhaseScanComplete,
		Total: len(toAdd),
	})

	// ── Phase 1: Blob staging (parallel, GOMAXPROCS workers) ──────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workersCount := addWorkerCount(len(toAdd))
	jobs := orderedIndexJobs(ctx, toAdd)
	preparedResults := make(chan indexedResult[preparedAddEntry], workersCount)
	var workers sync.WaitGroup
	for worker := 0; worker < workersCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				entry, content, err := r.prepareBlobEntry(job.value, opts)
				select {
				case preparedResults <- indexedResult[preparedAddEntry]{
					index: job.index,
					value: preparedAddEntry{entry: entry, content: content, err: err},
				}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	blobDone := make(chan struct{})
	go func() {
		workers.Wait()
		close(preparedResults)
		close(blobDone)
	}()

	blobs := make([]blobResult, len(toAdd))
	pending := make(map[int]preparedAddEntry, workersCount)
	for i, relPath := range toAdd {
		emitAddProgress(progress, AddProgress{
			Phase:   AddProgressPhaseStageFile,
			Path:    relPath,
			Current: i + 1,
			Total:   len(toAdd),
		})
		prepared, err := awaitOrderedResult(ctx, preparedResults, pending, i)
		if err != nil {
			cancel()
			<-blobDone
			return fmt.Errorf("add: %w", err)
		}
		blobs[i] = blobResult{
			relPath: relPath,
			entry:   prepared.entry,
			content: prepared.content,
		}
		stg.Entries[relPath] = prepared.entry
	}
	<-blobDone

	// Release Phase 1 content references so GC can reclaim before Phase 2.
	// Phase 2 re-reads from the blob store as needed.
	for i := range blobs {
		blobs[i].content = nil
	}

	// ── Phase 2: Entity extraction (bounded concurrency) ──────────────
	if !opts.SkipEntities {
		emitAddProgress(progress, AddProgress{
			Phase: AddProgressPhaseEntityStart,
			Total: len(blobs),
		})

		sem := newSourceBytesSemaphore(entityMemoryBudgetMB())
		ewCount := entityWorkerCount()
		entityJobs := make(chan int, ewCount)
		var entityWg sync.WaitGroup
		var entityErr error
		var entityErrOnce sync.Once

		for w := 0; w < ewCount; w++ {
			entityWg.Add(1)
			go func() {
				defer entityWg.Done()
				for idx := range entityJobs {
					br := &blobs[idx]
					if err := r.extractAndStoreEntities(ctx, sem, br, opts); err != nil {
						entityErrOnce.Do(func() { entityErr = err })
						return
					}
				}
			}()
		}

		for i := range blobs {
			select {
			case entityJobs <- i:
			case <-ctx.Done():
				break
			}
		}
		close(entityJobs)
		entityWg.Wait()

		if entityErr != nil {
			return fmt.Errorf("add: %w", entityErr)
		}

		// Emit per-file entity progress events and update staging entries.
		for i, br := range blobs {
			stg.Entries[br.relPath] = br.entry
			emitAddProgress(progress, AddProgress{
				Phase:   AddProgressPhaseEntityFile,
				Path:    br.relPath,
				Current: i + 1,
				Total:   len(blobs),
			})
		}

		emitAddProgress(progress, AddProgress{
			Phase: AddProgressPhaseEntityComplete,
			Total: len(blobs),
		})
	}

	emitAddProgress(progress, AddProgress{
		Phase:   AddProgressPhaseWriteIndex,
		Current: len(toAdd),
		Total:   len(toAdd),
	})
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("add: %w", err)
	}

	// Unified staging: also stage in git if a .git/ directory exists.
	r.gitStageFiles(toAdd)

	return nil
}

// gitStageFiles stages files in the colocated git repo (if present).
// This keeps git and graft staging areas in sync so that either
// `git commit` or `graft commit` produces consistent results.
// Errors are silently ignored — git staging is best-effort.
func (r *Repo) gitStageFiles(paths []string) {
	gitDir := filepath.Join(r.RootDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return // no git repo
	}
	if len(paths) == 0 {
		return
	}
	// Use git update-index --add --stdin to batch stage files.
	cmd := exec.Command("git", "add", "--")
	cmd.Args = append(cmd.Args, paths...)
	cmd.Dir = r.RootDir
	_ = cmd.Run()
}

// extractAndStoreEntities performs entity extraction for a single blob result,
// guarded by the source-bytes semaphore. It updates br.entry.EntityListHash
// in place and calls the AddHook if set.
func (r *Repo) extractAndStoreEntities(ctx context.Context, sem *sourceBytesSemaphore, br *blobResult, opts AddOptions) error {
	// Read content from the blob store instead of retaining it in memory
	// across phases. This prevents holding all file contents simultaneously.
	var content []byte
	if br.content != nil {
		content = br.content
		br.content = nil // allow GC
	} else {
		blob, err := r.Store.ReadBlob(br.entry.BlobHash)
		if err != nil {
			return nil // blob missing, skip entity extraction
		}
		content = blob.Data
	}
	if len(content) == 0 {
		return nil
	}

	// Detect language to check the denylist before acquiring semaphore.
	langEntry := grammars.DetectLanguage(br.relPath)
	if langEntry == nil {
		return nil // unsupported file type — no entities
	}
	if entity.ShouldSkipExtraction(langEntry.Name, int64(len(content)), opts.ForceEntities) {
		return nil
	}

	n := int64(len(content))
	if err := sem.Acquire(ctx, n); err != nil {
		return err
	}
	defer sem.Release(n)

	el, extractErr := entity.ExtractWithOptions(br.relPath, content, entity.ExtractOptions{
		ForceEntities: opts.ForceEntities,
	})
	if extractErr != nil {
		if errors.Is(extractErr, entity.ErrDataFormatSkipped) {
			return nil
		}
		// Non-fatal: unsupported file types, parse errors, etc.
		return nil
	}
	if len(el.Entities) == 0 {
		return nil
	}

	entityListHash, err := r.writeEntityList(br.relPath, el)
	if err != nil {
		return fmt.Errorf("write entities %q: %w", br.relPath, err)
	}
	br.entry.EntityListHash = entityListHash

	// Call the coordination hook with entity identity keys.
	if r.AddHook != nil {
		keys := make([]string, 0, len(el.Entities))
		for i := range el.Entities {
			keys = append(keys, el.Entities[i].IdentityKey())
		}
		_ = r.AddHook(br.relPath, keys)
	}

	return nil
}

func emitAddProgress(progress AddProgressFunc, event AddProgress) {
	if progress == nil {
		return
	}
	progress(event)
}

type indexedResult[T any] struct {
	index int
	value T
}

func orderedIndexJobs[T any](ctx context.Context, values []T) <-chan indexedResult[T] {
	jobs := make(chan indexedResult[T])
	go func() {
		defer close(jobs)
		for idx, value := range values {
			select {
			case jobs <- indexedResult[T]{index: idx, value: value}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return jobs
}

func awaitOrderedResult(
	ctx context.Context,
	results <-chan indexedResult[preparedAddEntry],
	pending map[int]preparedAddEntry,
	index int,
) (preparedAddEntry, error) {
	for {
		if prepared, ok := pending[index]; ok {
			delete(pending, index)
			return prepared, prepared.err
		}

		select {
		case result, ok := <-results:
			if !ok {
				return preparedAddEntry{}, fmt.Errorf("parallel add preparation ended early for index %d", index)
			}
			pending[result.index] = result.value
		case <-ctx.Done():
			return preparedAddEntry{}, ctx.Err()
		}
	}
}

func addWorkerCount(total int) int {
	if total <= 1 {
		return total
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > total {
		return total
	}
	return workers
}

// prepareBlobEntry performs Phase 1 work for a single file: read, stat, LFS
// check, and blob write. It returns the staging entry (with BlobHash set,
// EntityListHash empty) and the raw content for Phase 2 entity extraction.
func (r *Repo) prepareBlobEntry(relPath string, opts AddOptions) (*StagingEntry, []byte, error) {
	absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %q: %w", relPath, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %q: %w", relPath, err)
	}

	// LFS: if file is tracked via .graftattributes filter=lfs,
	// store actual content in LFS and replace with pointer.
	if r.IsLFSTracked(relPath) {
		oid, err := r.StoreLFSObject(content)
		if err != nil {
			return nil, nil, fmt.Errorf("lfs store %q: %w", relPath, err)
		}
		content = WriteLFSPointer(oid, int64(len(content)))
	}

	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: content})
	if err != nil {
		return nil, nil, fmt.Errorf("write blob %q: %w", relPath, err)
	}

	entry := &StagingEntry{
		Path:     relPath,
		BlobHash: blobHash,
	}
	setStagingEntryStat(entry, info, modeFromFileInfo(info))
	return entry, content, nil
}

// Remove stages file deletions and optionally removes files from disk.
func (r *Repo) Remove(paths []string, cached bool) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("rm: %w", err)
	}

	toRemove, err := r.expandRemovePaths(paths, stg)
	if err != nil {
		return fmt.Errorf("rm: %w", err)
	}
	if len(toRemove) == 0 {
		return fmt.Errorf("rm: no tracked files matched")
	}

	for _, relPath := range toRemove {
		delete(stg.Entries, relPath)
		if cached {
			continue
		}

		absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
		if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rm: remove %q: %w", relPath, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("rm: %w", err)
	}
	return nil
}

// writeEntityList writes each entity as an EntityObj to the store, collects
// their hashes, then writes and returns the hash of the EntityListObj.
func (r *Repo) writeEntityList(relPath string, el *entity.EntityList) (object.Hash, error) {
	var refs []object.Hash
	for _, ent := range el.Entities {
		entObj := &object.EntityObj{
			Kind:     ent.Kind.String(),
			Name:     ent.Name,
			DeclKind: ent.DeclKind,
			Receiver: ent.Receiver,
			Body:     ent.Body,
			BodyHash: object.Hash(ent.BodyHash),
		}
		h, err := r.Store.WriteEntity(entObj)
		if err != nil {
			return "", fmt.Errorf("write entity %q in %q: %w", ent.Name, relPath, err)
		}
		refs = append(refs, h)
	}

	elObj := &object.EntityListObj{
		Language:   el.Language,
		Path:       relPath,
		EntityRefs: refs,
	}
	return r.Store.WriteEntityList(elObj)
}

// repoRelPath converts a path (absolute, or relative to CWD) into a path
// relative to the repository root. If the path is already relative and does
// not start with the repo root, it is assumed to already be repo-relative.
func (r *Repo) repoRelPath(p string) (string, error) {
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(r.RootDir, p)
		if err != nil {
			return "", fmt.Errorf("cannot make %q relative to %q: %w", p, r.RootDir, err)
		}
		return filepath.ToSlash(rel), nil
	}

	// Try to resolve via CWD.
	cwd, err := os.Getwd()
	if err != nil {
		// Fall through to treating p as repo-relative.
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	abs := filepath.Join(cwd, p)
	// Check if the absolute path lives within the repo root.
	rel, err := filepath.Rel(r.RootDir, abs)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	// If the relative path starts with "..", p is outside the repo.
	// In that case, treat the original p as already repo-relative.
	if len(rel) >= 2 && rel[:2] == ".." {
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	return filepath.ToSlash(rel), nil
}

func (r *Repo) expandAddPaths(inputs []string) ([]string, error) {
	ic := NewIgnoreChecker(r.RootDir)
	seen := make(map[string]struct{})

	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if hasGlobMeta(input) {
			// If the path exists literally (e.g. Next.js [owner]/page.tsx with bracket
			// chars that would otherwise be interpreted as glob syntax), treat it as a
			// plain path rather than a glob pattern.
			if _, statErr := os.Stat(filepath.Join(r.RootDir, filepath.FromSlash(input))); statErr == nil {
				if err := r.collectAddPath(input, ic, seen); err != nil {
					return nil, fmt.Errorf("add %q: %w", input, err)
				}
				continue
			}

			spec, err := r.repoRelPath(input)
			if err != nil {
				return nil, fmt.Errorf("resolve path %q: %w", input, err)
			}
			if isOutsideRepo(spec) {
				return nil, fmt.Errorf("path %q is outside repository", input)
			}
			var matches []string
			if strings.Contains(spec, "**") {
				matches, err = r.globWithGlobstar(spec, ic)
				if err != nil {
					return nil, fmt.Errorf("glob %q: %w", input, err)
				}
			} else {
				globPattern := filepath.Join(r.RootDir, filepath.FromSlash(spec))
				matches, err = filepath.Glob(globPattern)
				if err != nil {
					return nil, fmt.Errorf("glob %q: %w", input, err)
				}
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("pathspec %q did not match any files", input)
			}
			for _, m := range matches {
				if err := r.collectAddPath(m, ic, seen); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := r.collectAddPath(input, ic, seen); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (r *Repo) collectAddPath(input string, ic *IgnoreChecker, seen map[string]struct{}) error {
	relPath, err := r.repoRelPath(input)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", input, err)
	}
	if isOutsideRepo(relPath) {
		return fmt.Errorf("path %q is outside repository", input)
	}
	if relPath == "." {
		relPath = ""
	}

	absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat %q: %w", relPath, err)
	}
	if !info.IsDir() {
		rel := filepath.ToSlash(relPath)
		if ic.IsIgnored(rel) {
			return nil
		}
		seen[rel] = struct{}{}
		return nil
	}

	return filepath.WalkDir(absPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ic.IsIgnored(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if ic.IsIgnored(rel) {
			return nil
		}
		seen[rel] = struct{}{}
		return nil
	})
}

func (r *Repo) expandRemovePaths(inputs []string, stg *Staging) ([]string, error) {
	tracked := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		tracked = append(tracked, filepath.ToSlash(p))
	}
	sort.Strings(tracked)

	seen := make(map[string]struct{})
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		spec, err := r.repoRelPath(input)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", input, err)
		}
		spec = filepath.ToSlash(spec)
		if isOutsideRepo(spec) {
			return nil, fmt.Errorf("path %q is outside repository", input)
		}

		matched := false
		if spec == "." || spec == "" {
			for _, p := range tracked {
				seen[p] = struct{}{}
			}
			matched = len(tracked) > 0
		} else if hasGlobMeta(spec) {
			for _, p := range tracked {
				if matchPathspec(spec, p) {
					seen[p] = struct{}{}
					matched = true
				}
			}
		} else {
			for _, p := range tracked {
				if p == spec || strings.HasPrefix(p, spec+"/") {
					seen[p] = struct{}{}
					matched = true
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("pathspec %q did not match tracked files", input)
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (r *Repo) globWithGlobstar(spec string, ic *IgnoreChecker) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(r.RootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ic.IsIgnored(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if ic.IsIgnored(rel) {
			return nil
		}
		if matchPathspec(spec, rel) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func matchPathspec(spec, path string) bool {
	spec = filepath.ToSlash(spec)
	path = filepath.ToSlash(path)
	if strings.Contains(spec, "**") {
		re, err := regexp.Compile(globPatternToRegex(spec))
		if err != nil {
			return false
		}
		return re.MatchString(path)
	}
	if strings.Contains(spec, "/") {
		ok, _ := filepath.Match(spec, path)
		return ok
	}
	ok, _ := filepath.Match(spec, filepath.Base(path))
	return ok
}

func isOutsideRepo(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	return rel == ".." || strings.HasPrefix(rel, "../")
}

func globPatternToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
				continue
			}
			b.WriteString("[^/]*")
			continue
		}
		if ch == '?' {
			b.WriteString("[^/]")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, rune(ch)) {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteString("$")
	return b.String()
}

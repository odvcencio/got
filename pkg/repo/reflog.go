package repo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

type ReflogEntry struct {
	Ref       string
	OldHash   object.Hash
	NewHash   object.Hash
	Timestamp int64
	Reason    string
}

// EntityChange records a single entity-level change in a ref update.
type EntityChange struct {
	Path       string // file path the entity belongs to
	EntityKey  string // entity identifier (e.g., "func:MyHandler" or "type:Config")
	ChangeType string // "create", "modify", or "delete"
}

// ReflogEntryWithEntities extends ReflogEntry with entity-level change data.
type ReflogEntryWithEntities struct {
	ReflogEntry
	Entities []EntityChange
}

func (r *Repo) appendReflog(ref string, oldHash, newHash object.Hash, reason string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "update"
	}

	// Reflogs for refs live alongside the refs — in the shared directory
	// for linked worktrees.
	baseDir := r.refsBaseDir()
	if ref == "HEAD" {
		baseDir = r.GraftDir
	}
	logPath := filepath.Join(baseDir, "logs", filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("reflog mkdir: %w", err)
	}

	old := string(oldHash)
	if strings.TrimSpace(old) == "" {
		old = zeroHash
	}
	newVal := string(newHash)
	if strings.TrimSpace(newVal) == "" {
		newVal = zeroHash
	}
	line := fmt.Sprintf("%s %s %d %s\n", old, newVal, time.Now().Unix(), reason)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("reflog open: %w", err)
	}

	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("reflog write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("reflog close: %w", err)
	}
	return nil
}

// appendReflogWithEntities writes an entity-enriched reflog entry.
// Format: same as normal reflog line, but append tab + entity data after reason:
//
//	<old> <new> <timestamp> <reason>\t<path>:<key>:<change>,<path>:<key>:<change>,...
//
// If entities is nil/empty, writes a normal reflog line (backward compatible).
func (r *Repo) appendReflogWithEntities(ref string, oldHash, newHash object.Hash, reason string, entities []EntityChange) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "update"
	}

	baseDir := r.refsBaseDir()
	if ref == "HEAD" {
		baseDir = r.GraftDir
	}
	logPath := filepath.Join(baseDir, "logs", filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("reflog mkdir: %w", err)
	}

	old := string(oldHash)
	if strings.TrimSpace(old) == "" {
		old = zeroHash
	}
	newVal := string(newHash)
	if strings.TrimSpace(newVal) == "" {
		newVal = zeroHash
	}

	line := fmt.Sprintf("%s %s %d %s", old, newVal, time.Now().Unix(), reason)
	if len(entities) > 0 {
		parts := make([]string, len(entities))
		for i, ec := range entities {
			parts[i] = ec.Path + ":" + ec.EntityKey + ":" + ec.ChangeType
		}
		line += "\t" + strings.Join(parts, ",")
	}
	line += "\n"

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("reflog open: %w", err)
	}

	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("reflog write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("reflog close: %w", err)
	}
	return nil
}

// ReadReflogWithEntities reads reflog entries with entity data.
// It parses the tab-separated entity suffix if present. Old entries
// without entity data get an empty Entities slice.
func (r *Repo) ReadReflogWithEntities(ref string, limit int) ([]ReflogEntryWithEntities, error) {
	refName, err := r.resolveReflogRefName(ref)
	if err != nil {
		return nil, err
	}

	baseDir := r.refsBaseDir()
	if refName == "HEAD" {
		baseDir = r.GraftDir
	}
	logPath := filepath.Join(baseDir, "logs", filepath.FromSlash(refName))
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read reflog: %w", err)
	}
	defer f.Close()

	var entries []ReflogEntryWithEntities
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}

		reason := parts[3]
		var entities []EntityChange

		// Check for tab-separated entity data after the reason.
		if tabIdx := strings.Index(reason, "\t"); tabIdx >= 0 {
			entityData := reason[tabIdx+1:]
			reason = reason[:tabIdx]
			entities = parseEntityChanges(entityData)
		}

		entries = append(entries, ReflogEntryWithEntities{
			ReflogEntry: ReflogEntry{
				Ref:       refName,
				OldHash:   object.Hash(parts[0]),
				NewHash:   object.Hash(parts[1]),
				Timestamp: ts,
				Reason:    reason,
			},
			Entities: entities,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read reflog: %w", err)
	}

	// Return newest first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// parseEntityChanges parses the comma-separated entity change data.
// Format: path:key:changeType,path:key:changeType,...
// The entity key may itself contain colons (e.g., "declaration:Hello"),
// so the changeType is identified as the last colon-separated segment.
func parseEntityChanges(data string) []EntityChange {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil
	}
	items := strings.Split(data, ",")
	var changes []EntityChange
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		// The format is path:entityKey:changeType where entityKey can contain
		// colons. The changeType is always the last segment, and path is the
		// first. Everything between the first and last colon is the key.
		firstColon := strings.Index(item, ":")
		lastColon := strings.LastIndex(item, ":")
		if firstColon < 0 || firstColon == lastColon {
			continue
		}
		changes = append(changes, EntityChange{
			Path:       item[:firstColon],
			EntityKey:  item[firstColon+1 : lastColon],
			ChangeType: item[lastColon+1:],
		})
	}
	return changes
}

// diffTreeEntities compares two commit trees to find entity-level changes.
// It returns a list of creates, modifies, and deletes.
func diffTreeEntities(r *Repo, oldCommit, newCommit object.Hash) ([]EntityChange, error) {
	// Read new commit tree.
	newCommitObj, err := r.Store.ReadCommit(newCommit)
	if err != nil {
		return nil, fmt.Errorf("diffTreeEntities: read new commit %s: %w", newCommit, err)
	}
	newEntries, err := r.FlattenTree(newCommitObj.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("diffTreeEntities: flatten new tree: %w", err)
	}

	newByPath := make(map[string]TreeFileEntry, len(newEntries))
	for _, e := range newEntries {
		newByPath[e.Path] = e
	}

	// Handle initial commit (no old commit).
	isInitial := string(oldCommit) == "" || string(oldCommit) == zeroHash
	var oldByPath map[string]TreeFileEntry
	if !isInitial {
		oldCommitObj, err := r.Store.ReadCommit(oldCommit)
		if err != nil {
			return nil, fmt.Errorf("diffTreeEntities: read old commit %s: %w", oldCommit, err)
		}
		oldEntries, err := r.FlattenTree(oldCommitObj.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("diffTreeEntities: flatten old tree: %w", err)
		}
		oldByPath = make(map[string]TreeFileEntry, len(oldEntries))
		for _, e := range oldEntries {
			oldByPath[e.Path] = e
		}
	} else {
		oldByPath = make(map[string]TreeFileEntry)
	}

	// Collect all unique paths.
	allPaths := make(map[string]struct{})
	for p := range oldByPath {
		allPaths[p] = struct{}{}
	}
	for p := range newByPath {
		allPaths[p] = struct{}{}
	}

	var sortedPaths []string
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	var changes []EntityChange
	for _, path := range sortedPaths {
		oldEntry, inOld := oldByPath[path]
		newEntry, inNew := newByPath[path]

		// Skip files with no entity list on either side.
		oldHasEntities := inOld && oldEntry.EntityListHash != ""
		newHasEntities := inNew && newEntry.EntityListHash != ""
		if !oldHasEntities && !newHasEntities {
			continue
		}

		// Skip if entity list hash hasn't changed.
		if inOld && inNew && oldEntry.EntityListHash == newEntry.EntityListHash {
			continue
		}

		// Build old entity key -> hash map.
		oldEntityMap, err := buildEntityKeyMap(r, oldEntry.EntityListHash, oldHasEntities)
		if err != nil {
			return nil, fmt.Errorf("diffTreeEntities: read old entities for %s: %w", path, err)
		}

		// Build new entity key -> hash map.
		newEntityMap, err := buildEntityKeyMap(r, newEntry.EntityListHash, newHasEntities)
		if err != nil {
			return nil, fmt.Errorf("diffTreeEntities: read new entities for %s: %w", path, err)
		}

		// Compare: key in new but not old = create
		for key, newHash := range newEntityMap {
			oldHash, inOldMap := oldEntityMap[key]
			if !inOldMap {
				changes = append(changes, EntityChange{
					Path:       path,
					EntityKey:  key,
					ChangeType: "create",
				})
			} else if oldHash != newHash {
				changes = append(changes, EntityChange{
					Path:       path,
					EntityKey:  key,
					ChangeType: "modify",
				})
			}
		}

		// Key in old but not new = delete
		for key := range oldEntityMap {
			if _, inNewMap := newEntityMap[key]; !inNewMap {
				changes = append(changes, EntityChange{
					Path:       path,
					EntityKey:  key,
					ChangeType: "delete",
				})
			}
		}
	}

	return changes, nil
}

// buildEntityKeyMap reads an entity list and builds a map from entity key to entity hash.
func buildEntityKeyMap(r *Repo, entityListHash object.Hash, hasEntities bool) (map[string]object.Hash, error) {
	result := make(map[string]object.Hash)
	if !hasEntities || entityListHash == "" {
		return result, nil
	}

	el, err := r.Store.ReadEntityList(entityListHash)
	if err != nil {
		return nil, err
	}

	for _, ref := range el.EntityRefs {
		ent, err := r.Store.ReadEntity(ref)
		if err != nil {
			return nil, fmt.Errorf("read entity %s: %w", ref, err)
		}
		key := ent.Kind + ":" + ent.Name
		result[key] = ref
	}
	return result, nil
}

// appendReflogAutoEntities computes entity-level changes between two commits
// and writes an entity-enriched reflog entry. If diffing fails (e.g., old
// commit doesn't exist for initial commit), falls back to normal appendReflog.
func (r *Repo) appendReflogAutoEntities(ref string, oldHash, newHash object.Hash, reason string) error {
	entities, err := diffTreeEntities(r, oldHash, newHash)
	if err != nil {
		// Fall back to normal reflog on any diff error.
		return r.appendReflog(ref, oldHash, newHash, reason)
	}
	return r.appendReflogWithEntities(ref, oldHash, newHash, reason, entities)
}

func (r *Repo) ReadReflog(ref string, limit int) ([]ReflogEntry, error) {
	refName, err := r.resolveReflogRefName(ref)
	if err != nil {
		return nil, err
	}

	baseDir := r.refsBaseDir()
	if refName == "HEAD" {
		baseDir = r.GraftDir
	}
	logPath := filepath.Join(baseDir, "logs", filepath.FromSlash(refName))
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read reflog: %w", err)
	}
	defer f.Close()

	var entries []ReflogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, ReflogEntry{
			Ref:       refName,
			OldHash:   object.Hash(parts[0]),
			NewHash:   object.Hash(parts[1]),
			Timestamp: ts,
			Reason:    parts[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read reflog: %w", err)
	}

	// Return newest first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (r *Repo) resolveReflogRefName(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "HEAD" {
		head, err := r.Head()
		if err == nil && strings.HasPrefix(head, "refs/") {
			return head, nil
		}
		return "HEAD", nil
	}
	if strings.HasPrefix(ref, "refs/") {
		return ref, nil
	}
	return "refs/heads/" + ref, nil
}

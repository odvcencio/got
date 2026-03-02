package repo

import "fmt"

// defaultModuleMaxDepth is the maximum nesting depth for recursive module sync
// when no explicit limit is provided.
const defaultModuleMaxDepth = 10

// checkModuleCycle returns an error if url has already been visited,
// indicating a dependency cycle.
func checkModuleCycle(url string, visited map[string]bool) error {
	if visited[url] {
		return fmt.Errorf("module cycle detected: %s", url)
	}
	return nil
}

// checkDepthLimit returns an error if current exceeds max, indicating that the
// recursive module sync has exceeded the configured nesting depth.
func checkDepthLimit(current, max int) error {
	if current > max {
		return fmt.Errorf("module depth limit exceeded: depth %d > max %d", current, max)
	}
	return nil
}

// ModuleSyncRecursive syncs modules recursively with cycle detection.
// If maxDepth <= 0, defaultModuleMaxDepth is used.
func (r *Repo) ModuleSyncRecursive(maxDepth int) error {
	if maxDepth <= 0 {
		maxDepth = defaultModuleMaxDepth
	}
	visited := make(map[string]bool)
	return r.moduleSyncRecursiveInner(0, maxDepth, visited)
}

// moduleSyncRecursiveInner performs one level of recursive module sync.
//
//  1. Check depth limit.
//  2. Read .graftmodules and .graftmodules.lock.
//  3. For each module:
//     a. Check cycle (by URL).
//     b. Mark URL as visited.
//     c. Sync module (if locked).
//
// The structure is in place for future recursive descent into nested modules.
func (r *Repo) moduleSyncRecursiveInner(depth, maxDepth int, visited map[string]bool) error {
	if err := checkDepthLimit(depth, maxDepth); err != nil {
		return fmt.Errorf("recursive module sync: %w", err)
	}

	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return fmt.Errorf("recursive module sync: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return fmt.Errorf("recursive module sync: %w", err)
	}
	if lock == nil {
		return nil
	}

	for _, entry := range entries {
		if err := checkModuleCycle(entry.URL, visited); err != nil {
			return fmt.Errorf("recursive module sync: %s: %w", entry.Name, err)
		}
		visited[entry.URL] = true

		le, ok := lock.Modules[entry.Name]
		if !ok {
			// Module declared but not yet locked — skip.
			continue
		}

		if err := r.syncModule(entry, le); err != nil {
			return fmt.Errorf("recursive module sync: %s: %w", entry.Name, err)
		}

		// Future: open the sub-module as a Repo and recurse:
		// subRepo, err := OpenRepo(filepath.Join(r.RootDir, entry.Path))
		// if err == nil {
		//     if err := subRepo.moduleSyncRecursiveInner(depth+1, maxDepth, visited); err != nil {
		//         return fmt.Errorf("recursive module sync: %s: %w", entry.Name, err)
		//     }
		// }
	}

	return nil
}

package coord

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// GoModDeps holds parsed dependency information from a go.mod file.
type GoModDeps struct {
	Module   string
	Requires []string
	Replaces map[string]string // module path -> local path
}

// ParseGoModDeps parses a go.mod file to extract the module name, require
// directives, and replace directives (only local path replaces).
func ParseGoModDeps(gomodPath string) (*GoModDeps, error) {
	f, err := os.Open(gomodPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	deps := &GoModDeps{
		Replaces: make(map[string]string),
	}

	scanner := bufio.NewScanner(f)
	inRequireBlock := false
	inReplaceBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Handle block open/close.
		if line == ")" {
			inRequireBlock = false
			inReplaceBlock = false
			continue
		}

		if strings.HasPrefix(line, "require (") || line == "require (" {
			inRequireBlock = true
			continue
		}
		if strings.HasPrefix(line, "replace (") || line == "replace (" {
			inReplaceBlock = true
			continue
		}

		// Module directive.
		if strings.HasPrefix(line, "module ") {
			deps.Module = strings.TrimPrefix(line, "module ")
			deps.Module = strings.TrimSpace(deps.Module)
			continue
		}

		// Single-line require directive.
		if strings.HasPrefix(line, "require ") && !inRequireBlock {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				deps.Requires = append(deps.Requires, parts[1])
			}
			continue
		}

		// Inside require block.
		if inRequireBlock {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				deps.Requires = append(deps.Requires, parts[0])
			}
			continue
		}

		// Single-line replace directive.
		if strings.HasPrefix(line, "replace ") && !inReplaceBlock {
			parseReplace(line[len("replace "):], deps)
			continue
		}

		// Inside replace block.
		if inReplaceBlock {
			parseReplace(line, deps)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return deps, nil
}

// parseReplace parses a single replace directive line (without the "replace"
// keyword) and adds local path replacements to deps.Replaces.
func parseReplace(line string, deps *GoModDeps) {
	// Format: module/path [version] => replacement [version]
	// We only care about local path replacements (no version after =>).
	parts := strings.SplitN(line, "=>", 2)
	if len(parts) != 2 {
		return
	}

	leftFields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(leftFields) == 0 {
		return
	}
	modulePath := leftFields[0]

	replacement := strings.TrimSpace(parts[1])
	replFields := strings.Fields(replacement)
	if len(replFields) == 0 {
		return
	}

	target := replFields[0]

	// Only record local path replacements (starting with . or /).
	if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") {
		deps.Replaces[modulePath] = target
	}
}

// WorkspaceGraph represents dependency relationships between workspaces.
type WorkspaceGraph struct {
	// dependents maps a workspace name to the list of workspace names that
	// depend on it (reverse adjacency).
	dependents map[string][]string
}

// DependentsOf returns the workspace names that depend on the given workspace.
func (g *WorkspaceGraph) DependentsOf(workspace string) []string {
	return g.dependents[workspace]
}

// BuildWorkspaceGraph takes a map of workspace name -> path, parses each
// go.mod, and builds the dependency graph by matching require module paths to
// workspace module paths.
func BuildWorkspaceGraph(workspaces map[string]string) (*WorkspaceGraph, error) {
	// First pass: parse all go.mod files and build a module path -> workspace
	// name mapping.
	type wsInfo struct {
		name string
		deps *GoModDeps
	}

	moduleToWorkspace := make(map[string]string) // module path -> workspace name
	var allWs []wsInfo

	for name, wsPath := range workspaces {
		gomodPath := filepath.Join(wsPath, "go.mod")
		deps, err := ParseGoModDeps(gomodPath)
		if err != nil {
			return nil, err
		}
		moduleToWorkspace[deps.Module] = name
		allWs = append(allWs, wsInfo{name: name, deps: deps})
	}

	// Second pass: for each workspace, check if any of its require directives
	// match a known workspace module path. If so, record the dependency edge.
	graph := &WorkspaceGraph{
		dependents: make(map[string][]string),
	}

	for _, ws := range allWs {
		for _, req := range ws.deps.Requires {
			if depName, ok := moduleToWorkspace[req]; ok {
				graph.dependents[depName] = append(graph.dependents[depName], ws.name)
			}
		}
	}

	return graph, nil
}

// AutoDiscoverWorkspaces finds related workspaces by:
// 1. Parsing go.mod replace directives (local paths)
// 2. Scanning sibling directories for go.mod files
// Returns a map of workspace name -> absolute path.
func AutoDiscoverWorkspaces(repoDir string) (map[string]string, error) {
	discovered := make(map[string]string)

	// Resolve repoDir to absolute path for consistent comparison.
	absRepoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, err
	}

	// 1. Parse replace directives.
	gomodPath := filepath.Join(absRepoDir, "go.mod")
	if deps, err := ParseGoModDeps(gomodPath); err == nil {
		for _, localPath := range deps.Replaces {
			// Resolve relative paths against the repo directory.
			if !filepath.IsAbs(localPath) {
				localPath = filepath.Join(absRepoDir, localPath)
			}
			name := filepath.Base(localPath)
			discovered[name] = localPath
		}
	}

	// 2. Scan sibling directories.
	parent := filepath.Dir(absRepoDir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return discovered, nil // non-fatal
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		siblingPath := filepath.Join(parent, entry.Name())
		if siblingPath == absRepoDir {
			continue // skip self
		}
		// Check for go.mod.
		if _, err := os.Stat(filepath.Join(siblingPath, "go.mod")); err == nil {
			name := entry.Name()
			if _, exists := discovered[name]; !exists {
				discovered[name] = siblingPath
			}
		}
	}

	return discovered, nil
}

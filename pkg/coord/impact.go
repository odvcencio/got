package coord

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AnalyzeImpact computes cross-repo impact for a set of entity changes.
// It combines the workspace graph, export index, xref indices from
// downstream repos, and the claim registry to determine which workspaces
// and agents are affected by the changes.
//
// workspaces maps workspace name -> absolute directory path.
func (c *Coordinator) AnalyzeImpact(changes []EntityChange, workspaces map[string]string) (*ImpactReport, error) {
	report := &ImpactReport{
		Workspaces: make(map[string]WorkspaceImpact),
	}

	if len(changes) == 0 || len(workspaces) == 0 {
		return report, nil
	}

	// 1. Build workspace dependency graph.
	graph, err := BuildWorkspaceGraph(workspaces)
	if err != nil {
		return nil, fmt.Errorf("build workspace graph: %w", err)
	}

	// 2. Load the export index to identify which changes affect public API.
	exportIdx, err := c.LoadExportIndex()
	if err != nil {
		// No export index -- build one from current HEAD.
		exportIdx, err = BuildExportIndex(c.Repo)
		if err != nil {
			return nil, fmt.Errorf("build export index: %w", err)
		}
	}

	// 3. Determine the current workspace name by matching our repo dir.
	currentWorkspace := identifyCurrentWorkspace(c.Repo.RootDir, workspaces)

	// 4. Identify which changes affect exported entities.
	var affectedExportKeys []string
	for _, change := range changes {
		if isExportedChange(change, exportIdx) {
			affectedExportKeys = append(affectedExportKeys, change.Key)
		}
	}

	if len(affectedExportKeys) == 0 {
		// No exported entities changed -- no cross-repo impact.
		return report, nil
	}

	// 5. Find downstream workspaces.
	downstreamNames := graph.DependentsOf(currentWorkspace)
	if len(downstreamNames) == 0 {
		return report, nil
	}

	// 6. Get the module path of the current workspace (for qualified name lookup).
	currentModulePath := ""
	gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
	if deps, err := ParseGoModDeps(gomodPath); err == nil {
		currentModulePath = deps.Module
	}

	// 7. For each downstream workspace, load or build its xref index
	// and find callers of the changed exports.
	for _, wsName := range downstreamNames {
		wsPath, ok := workspaces[wsName]
		if !ok {
			continue
		}

		// Determine the downstream module path.
		wsModulePath := ""
		wsGomod := filepath.Join(wsPath, "go.mod")
		if deps, err := ParseGoModDeps(wsGomod); err == nil {
			wsModulePath = deps.Module
		}

		xrefIdx, err := BuildXrefIndex(wsPath, wsModulePath)
		if err != nil {
			continue // skip workspaces we can't scan
		}

		var callers []string

		for _, change := range changes {
			if !isExportedChange(change, exportIdx) {
				continue
			}

			// Build qualified names to look up in xref index.
			qualNames := buildQualifiedNames(change, exportIdx, currentModulePath)
			for _, qn := range qualNames {
				if sites, ok := xrefIdx.Refs[qn]; ok {
					for _, site := range sites {
						caller := fmt.Sprintf("%s:%s:%d", site.File, site.Entity, site.Line)
						callers = append(callers, caller)
					}
				}
			}
		}

		if len(callers) == 0 {
			continue
		}

		impact := WorkspaceImpact{
			Callers: callers,
		}

		// 8. Check if any agents in the downstream workspace are editing
		// entities that are callers of the changed exports.
		affectedAgents := c.findAffectedAgents(callers)
		if len(affectedAgents) > 0 {
			impact.AgentsAffected = affectedAgents
		}

		report.Workspaces[wsName] = impact
	}

	return report, nil
}

// identifyCurrentWorkspace finds the workspace name that matches the given
// repo directory path.
func identifyCurrentWorkspace(repoDir string, workspaces map[string]string) string {
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return ""
	}
	for name, wsPath := range workspaces {
		absWs, err := filepath.Abs(wsPath)
		if err != nil {
			continue
		}
		if absRepo == absWs {
			return name
		}
	}
	// Fallback: use directory basename.
	return filepath.Base(absRepo)
}

// isExportedChange checks if an entity change affects an exported symbol
// by looking it up in the export index.
func isExportedChange(change EntityChange, exportIdx *ExportIndex) bool {
	if exportIdx == nil {
		return false
	}

	// Try to match the change against any package's exported entities.
	for _, pkgEntities := range exportIdx.Packages {
		for key := range pkgEntities {
			if matchesEntityKey(change.Key, key) {
				return true
			}
		}
	}

	// Fallback: check if the entity name looks exported (starts with uppercase).
	name := extractNameFromKey(change.Key)
	return isExported(name)
}

// matchesEntityKey checks if a change key matches an export key.
// Change keys from the feed use the format "decl:kind::Name:sig:ordinal".
// Export keys use formats like "func:Name", "type:Name", "method:Recv.Name".
func matchesEntityKey(changeKey, exportKey string) bool {
	// Direct match.
	if changeKey == exportKey {
		return true
	}

	// Extract name from change key and export key and compare.
	changeName := extractNameFromKey(changeKey)
	exportName := extractNameFromKey(exportKey)

	if changeName != "" && exportName != "" && changeName == exportName {
		return true
	}

	return false
}

// extractNameFromKey extracts the entity name from various key formats.
func extractNameFromKey(key string) string {
	// Handle export index keys: "func:Name", "type:Name", "method:Recv.Name",
	// "var:Name", "const:Name"
	if strings.HasPrefix(key, "func:") {
		return strings.TrimPrefix(key, "func:")
	}
	if strings.HasPrefix(key, "type:") {
		return strings.TrimPrefix(key, "type:")
	}
	if strings.HasPrefix(key, "method:") {
		rest := strings.TrimPrefix(key, "method:")
		if idx := strings.LastIndex(rest, "."); idx >= 0 {
			return rest[idx+1:]
		}
		return rest
	}
	if strings.HasPrefix(key, "var:") {
		return strings.TrimPrefix(key, "var:")
	}
	if strings.HasPrefix(key, "const:") {
		return strings.TrimPrefix(key, "const:")
	}

	// Handle graft entity keys: "decl:kind:receiver:Name:sig:ordinal"
	if strings.HasPrefix(key, "decl:") {
		parts := strings.SplitN(key, ":", 6)
		if len(parts) >= 4 {
			return parts[3] // Name field
		}
	}

	return key
}

// buildQualifiedNames builds fully qualified symbol names for looking up
// in xref indices. For example, a function "HandleRequest" in package
// "pkg/handler" with module path "github.com/example/provider" becomes
// "github.com/example/provider/pkg/handler.HandleRequest".
func buildQualifiedNames(change EntityChange, exportIdx *ExportIndex, modulePath string) []string {
	var names []string

	entityName := extractNameFromKey(change.Key)
	if entityName == "" {
		return nil
	}

	for pkgPath, pkgEntities := range exportIdx.Packages {
		for key := range pkgEntities {
			exportName := extractNameFromKey(key)
			if exportName == entityName {
				// Build qualified name.
				var qualName string
				if modulePath != "" && pkgPath != "" {
					qualName = modulePath + "/" + pkgPath + "." + entityName
				} else if modulePath != "" {
					qualName = modulePath + "." + entityName
				} else if pkgPath != "" {
					qualName = pkgPath + "." + entityName
				} else {
					qualName = entityName
				}
				names = append(names, qualName)
			}
		}
	}

	return names
}

// findAffectedAgents checks the claim registry for agents editing entities
// that appear in the caller list.
func (c *Coordinator) findAffectedAgents(callers []string) []string {
	claims, err := c.ListClaims()
	if err != nil {
		return nil
	}

	agentSet := make(map[string]struct{})
	for _, claim := range claims {
		claimName := extractNameFromKey(claim.EntityKey)
		for _, caller := range callers {
			// caller format: "file:entity:line"
			parts := strings.SplitN(caller, ":", 3)
			if len(parts) >= 2 {
				callerEntity := parts[1]
				if claimName == callerEntity {
					agentSet[claim.AgentName] = struct{}{}
				}
			}
		}
	}

	var agents []string
	for name := range agentSet {
		agents = append(agents, name)
	}
	return agents
}

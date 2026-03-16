package repo

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// HookEntry describes a single hook defined in hooks.toml or user config.
type HookEntry struct {
	Name         string   `toml:"name"`
	Point        string   `toml:"point"`
	Run          string   `toml:"run"`
	Type         string   `toml:"type"`
	OnFail       string   `toml:"on_fail"`
	Timeout      string   `toml:"timeout"`
	Remote       string   `toml:"remote"`
	BranchFilter []string `toml:"branch_filter"`
	Source       string   `toml:"source"`
	Grep         string   `toml:"grep"`    // structural grep pattern (e.g. "go::$PKG.Exec($$$ARGS)")
	Action       string   `toml:"action"`  // "block" or "warn" (for grep hooks)
	Message      string   `toml:"message"` // human-readable message shown on match
}

// HooksConfig holds all hook entries loaded from repo and user configuration.
type HooksConfig struct {
	Hooks []HookEntry
}

// ForPoint returns all hooks registered for the given trigger point.
func (c *HooksConfig) ForPoint(point string) []HookEntry {
	var result []HookEntry
	for _, h := range c.Hooks {
		if h.Point == point {
			result = append(result, h)
		}
	}
	return result
}

// rawHooksFile is the intermediate representation of hooks.toml.
// The TOML format is:
//
//	[point.name]
//	run = "cmd"
//	timeout = "5s"
type rawHooksFile map[string]map[string]HookEntry

// LoadHooksConfig reads hooks.toml from repoRoot (if it exists) and merges
// user hooks. Repo hooks take precedence: user hooks with the same
// point+name pair are silently dropped. The returned config is sorted by
// point then name for deterministic ordering.
func LoadHooksConfig(repoRoot string, userHooks map[string]map[string]HookEntry) (*HooksConfig, error) {
	cfg := &HooksConfig{}

	// Track which point+name pairs come from the repo file.
	repoKeys := make(map[string]struct{})

	// Try loading the repo hooks.toml.
	tomlPath := filepath.Join(repoRoot, "hooks.toml")
	data, err := os.ReadFile(tomlPath)
	if err == nil {
		var raw rawHooksFile
		if err := toml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		for point, entries := range raw {
			for name, entry := range entries {
				entry.Point = point
				entry.Name = name
				if entry.Source == "" {
					entry.Source = "repo"
				}
				cfg.Hooks = append(cfg.Hooks, entry)
				repoKeys[point+"\x00"+name] = struct{}{}
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Merge user hooks (additive, repo wins on conflict).
	for point, entries := range userHooks {
		for name, entry := range entries {
			key := point + "\x00" + name
			if _, exists := repoKeys[key]; exists {
				// Repo hook takes precedence; silently drop.
				continue
			}
			entry.Point = point
			entry.Name = name
			if entry.Source == "" {
				entry.Source = "user"
			}
			cfg.Hooks = append(cfg.Hooks, entry)
		}
	}

	// Sort by point then name for deterministic ordering.
	sort.Slice(cfg.Hooks, func(i, j int) bool {
		if cfg.Hooks[i].Point != cfg.Hooks[j].Point {
			return cfg.Hooks[i].Point < cfg.Hooks[j].Point
		}
		return cfg.Hooks[i].Name < cfg.Hooks[j].Name
	})

	return cfg, nil
}

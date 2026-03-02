package repo

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ModuleEntry describes a single module declared in .graftmodules.
type ModuleEntry struct {
	Name  string // unique identifier
	URL   string // remote URL (supports graft shorthand)
	Path  string // working tree path relative to repo root
	Track string // branch to follow (mutually exclusive with Pin)
	Pin   string // tag or commit to lock to (mutually exclusive with Track)
}

// knownModuleKeys is the set of valid keys inside a [module "name"] section.
var knownModuleKeys = map[string]bool{
	"url":   true,
	"path":  true,
	"track": true,
	"pin":   true,
}

// ParseGraftModules parses INI-style .graftmodules content from r.
//
// Section format: [module "name"]
// Keys: url, path, track, pin
// Comments: lines starting with # or ;
// Inline comments: ` #` or ` ;` after value
func ParseGraftModules(r io.Reader) ([]ModuleEntry, error) {
	scanner := bufio.NewScanner(r)
	var modules []ModuleEntry
	var current *ModuleEntry

	namesSeen := make(map[string]bool)
	pathsSeen := make(map[string]bool)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Strip leading/trailing whitespace.
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comment-only lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		// Check for section header: [module "name"]
		if strings.HasPrefix(trimmed, "[") {
			// Finalize previous section.
			if current != nil {
				if err := validateModuleEntry(current); err != nil {
					return nil, fmt.Errorf("line %d: module %q: %w", lineNum, current.Name, err)
				}
				modules = append(modules, *current)
				current = nil
			}

			name, err := parseSectionHeader(trimmed, lineNum)
			if err != nil {
				return nil, err
			}
			if namesSeen[name] {
				return nil, fmt.Errorf("line %d: duplicate module name %q", lineNum, name)
			}
			namesSeen[name] = true
			current = &ModuleEntry{Name: name}
			continue
		}

		// Must be a key = value line inside a section.
		if current == nil {
			return nil, fmt.Errorf("line %d: key-value pair outside of section", lineNum)
		}

		key, value, err := parseKeyValue(trimmed, lineNum)
		if err != nil {
			return nil, err
		}

		if !knownModuleKeys[key] {
			return nil, fmt.Errorf("line %d: unknown key %q in module %q", lineNum, key, current.Name)
		}

		switch key {
		case "url":
			current.URL = value
		case "path":
			current.Path = value
		case "track":
			current.Track = value
		case "pin":
			current.Pin = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .graftmodules: %w", err)
	}

	// Finalize last section.
	if current != nil {
		if err := validateModuleEntry(current); err != nil {
			return nil, fmt.Errorf("module %q: %w", current.Name, err)
		}
		modules = append(modules, *current)
	}

	// Check for duplicate paths across all modules.
	for _, m := range modules {
		if pathsSeen[m.Path] {
			return nil, fmt.Errorf("duplicate module path %q", m.Path)
		}
		pathsSeen[m.Path] = true
	}

	return modules, nil
}

// parseSectionHeader parses a line like `[module "name"]` and returns the name.
func parseSectionHeader(line string, lineNum int) (string, error) {
	if !strings.HasSuffix(line, "]") {
		return "", fmt.Errorf("line %d: malformed section header", lineNum)
	}

	inner := line[1 : len(line)-1] // strip [ and ]
	inner = strings.TrimSpace(inner)

	if !strings.HasPrefix(inner, "module ") {
		return "", fmt.Errorf("line %d: unsupported section type (expected [module \"name\"])", lineNum)
	}

	nameQuoted := strings.TrimSpace(strings.TrimPrefix(inner, "module"))
	if len(nameQuoted) < 2 || nameQuoted[0] != '"' || nameQuoted[len(nameQuoted)-1] != '"' {
		return "", fmt.Errorf("line %d: module name must be quoted (expected [module \"name\"])", lineNum)
	}

	name := nameQuoted[1 : len(nameQuoted)-1]
	if name == "" {
		return "", fmt.Errorf("line %d: module name cannot be empty", lineNum)
	}
	return name, nil
}

// parseKeyValue parses a line like `key = value` or `key = value # comment`.
func parseKeyValue(line string, lineNum int) (string, string, error) {
	eqIdx := strings.IndexByte(line, '=')
	if eqIdx < 0 {
		return "", "", fmt.Errorf("line %d: expected key = value", lineNum)
	}

	key := strings.TrimSpace(line[:eqIdx])
	value := strings.TrimSpace(line[eqIdx+1:])

	// Strip inline comments: ` #` or ` ;` (space required before comment char).
	value = stripInlineComment(value)

	if key == "" {
		return "", "", fmt.Errorf("line %d: empty key", lineNum)
	}

	return key, value, nil
}

// stripInlineComment removes an inline comment starting with ` #` or ` ;`.
func stripInlineComment(value string) string {
	for _, marker := range []string{" #", " ;"} {
		if idx := strings.Index(value, marker); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
	}
	return value
}

// validateModuleEntry checks that a completed ModuleEntry has the required
// fields and does not violate mutual-exclusion rules.
func validateModuleEntry(e *ModuleEntry) error {
	if e.URL == "" {
		return fmt.Errorf("url is required")
	}
	if e.Path == "" {
		return fmt.Errorf("path is required")
	}
	if e.Track != "" && e.Pin != "" {
		return fmt.Errorf("track and pin are mutually exclusive")
	}
	return nil
}

// WriteGraftModules writes modules in INI format to w.
func WriteGraftModules(w io.Writer, modules []ModuleEntry) error {
	for i, m := range modules {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "[module %q]\n", m.Name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "\turl = %s\n", m.URL); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "\tpath = %s\n", m.Path); err != nil {
			return err
		}
		if m.Track != "" {
			if _, err := fmt.Fprintf(w, "\ttrack = %s\n", m.Track); err != nil {
				return err
			}
		}
		if m.Pin != "" {
			if _, err := fmt.Fprintf(w, "\tpin = %s\n", m.Pin); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadGraftModulesFile reads and parses .graftmodules from the repository root.
// If the file does not exist, it returns nil, nil.
func (r *Repo) ReadGraftModulesFile() ([]ModuleEntry, error) {
	p := filepath.Join(r.RootDir, ".graftmodules")
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read .graftmodules: %w", err)
	}
	defer f.Close()

	modules, err := ParseGraftModules(f)
	if err != nil {
		return nil, fmt.Errorf("read .graftmodules: %w", err)
	}
	return modules, nil
}

// WriteGraftModulesFile atomically writes .graftmodules to the repository root.
func (r *Repo) WriteGraftModulesFile(modules []ModuleEntry) error {
	p := filepath.Join(r.RootDir, ".graftmodules")

	tmp, err := os.CreateTemp(r.RootDir, ".graftmodules-tmp-*")
	if err != nil {
		return fmt.Errorf("write .graftmodules: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if err := WriteGraftModules(tmp, modules); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write .graftmodules: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write .graftmodules: close: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write .graftmodules: rename: %w", err)
	}
	return nil
}

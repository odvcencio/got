// Package grammars provides 206 embedded tree-sitter grammars as compressed
// binary blobs with lazy loading. Use AllLanguages to enumerate available
// grammars, DetectLanguage to match by file extension or shebang, or call
// individual language functions (e.g. GoLanguage()) for direct access.
package grammars

import (
	"path"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

// LangEntry holds a registered language with its grammar, extensions, and highlight query.
type LangEntry struct {
	Name               string
	Extensions         []string                      // e.g. [".go", ".mod"]
	Shebangs           []string                      // e.g. ["#!/usr/bin/env python"]
	Language           func() *gotreesitter.Language // lazy loader
	HighlightQuery     string
	TagsQuery          string                                                                 // tree-sitter tags.scm query for symbol extraction
	TokenSourceFactory func(src []byte, lang *gotreesitter.Language) gotreesitter.TokenSource // nil = use DFA
	Quality            ParseQuality                                                           // populated lazily by AllLanguages
}

var registry []LangEntry

// Register adds a language to the registry.
func Register(entry LangEntry) {
	if !languageEnabled(entry.Name) {
		return
	}
	if entry.TokenSourceFactory == nil {
		entry.TokenSourceFactory = defaultTokenSourceFactory(entry.Name)
	}
	registry = append(registry, entry)
}

// DetectLanguage returns the LangEntry for a filename, or nil if unknown.
// Checks in order: exact filename match (linguist), registry extensions,
// then linguist extended extensions. Exact filenames take priority over
// suffix matching so that e.g. ".tmux.conf" resolves to bash rather than
// matching the generic ".conf" extension.
func DetectLanguage(filename string) *LangEntry {
	// 1. Exact filename match (e.g., "Makefile", "Dockerfile", ".bashrc",
	//    "nginx.conf"). Most specific, so checked first.
	base := path.Base(filename)
	if grammarName, ok := linguistFilenames[base]; ok {
		return lookupByName(grammarName)
	}

	// 2. Match by registry extensions (from languages.manifest).
	for i := range registry {
		for _, ext := range registry[i].Extensions {
			if strings.HasSuffix(filename, ext) {
				return &registry[i]
			}
		}
	}

	// 3. Linguist extended extensions (e.g., ".mk" for make, ".rake" for ruby).
	ext := strings.ToLower(path.Ext(filename))
	if ext != "" {
		if grammarName, ok := linguistExtensions[ext]; ok {
			return lookupByName(grammarName)
		}
	}

	return nil
}

// DetectLanguageByShebang checks the first line of content for shebang matches.
// Handles both "#!/usr/bin/env python3" and "#!/usr/bin/python3" forms.
func DetectLanguageByShebang(firstLine string) *LangEntry {
	// 1. Registry shebangs (exact prefix match).
	for i := range registry {
		for _, shebang := range registry[i].Shebangs {
			if strings.HasPrefix(firstLine, shebang) {
				return &registry[i]
			}
		}
	}

	// 2. Extract interpreter from shebang and look up in linguist map.
	interp := extractInterpreter(firstLine)
	if interp != "" {
		if grammarName, ok := linguistInterpreters[interp]; ok {
			return lookupByName(grammarName)
		}
	}

	return nil
}

// extractInterpreter parses a shebang line and returns the interpreter name.
// Handles "#!/usr/bin/env python3" → "python3" and "#!/usr/bin/python3" → "python3".
func extractInterpreter(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#!") {
		return ""
	}
	line = line[2:]
	line = strings.TrimSpace(line)

	// Split into path and args.
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}

	// Get the binary name from the path.
	binary := path.Base(parts[0])

	// If it's "env", the interpreter is the next non-flag, non-VAR=val argument.
	if binary == "env" {
		for _, arg := range parts[1:] {
			if strings.HasPrefix(arg, "-") {
				continue // skip flags like -S, -u
			}
			if strings.Contains(arg, "=") {
				continue // skip VAR=value env assignments
			}
			return strings.ToLower(arg)
		}
		return ""
	}

	return strings.ToLower(binary)
}

// AllLanguages returns all registered languages.
func AllLanguages() []LangEntry {
	out := make([]LangEntry, len(registry))
	copy(out, registry)
	for i := range out {
		if strings.TrimSpace(out[i].TagsQuery) != "" {
			continue
		}
		out[i].TagsQuery = inferredTagsQuery(out[i])
	}
	return out
}

// lookupByName returns the LangEntry with the given grammar name, or nil.
func lookupByName(name string) *LangEntry {
	for i := range registry {
		if registry[i].Name == name {
			return &registry[i]
		}
	}
	return nil
}

// normalizeLinguistKey lowercases and trims input, preserving special
// characters (+, #, etc.) so "C++" and "F#" map correctly.
func normalizeLinguistKey(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// DetectLanguageByName returns the LangEntry for any linguist canonical name,
// alias, or gotreesitter grammar name. Returns nil if unknown.
//
// Accepts: "C++", "cpp", "Go", "golang", "Shell", "bash", "F#", "fsharp", etc.
// Direct grammar names always take priority over linguist aliases to prevent
// shadowing (e.g., "eex" resolves to the eex grammar, not heex via alias).
func DetectLanguageByName(name string) *LangEntry {
	key := normalizeLinguistKey(name)
	// Direct grammar name takes priority over alias mapping.
	if entry := lookupByName(key); entry != nil {
		return entry
	}
	if grammarName, ok := linguistToGrammar[key]; ok {
		return lookupByName(grammarName)
	}
	return nil
}

// DisplayName returns the linguist canonical display name for a language
// (e.g., "C++" for cpp, "JavaScript" for javascript). Falls back to
// title-casing the grammar name if no linguist match exists.
func DisplayName(entry *LangEntry) string {
	if entry == nil {
		return ""
	}
	if dn, ok := grammarDisplayNames[entry.Name]; ok {
		return dn
	}
	// Fallback: title-case with underscores as spaces.
	words := strings.Split(entry.Name, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

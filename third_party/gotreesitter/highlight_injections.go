package gotreesitter

import (
	"strings"
	"sync"
)

// HighlighterInjectionResolver maps a language hint (for example "go" from a
// markdown code fence) to a child language and highlight query.
type HighlighterInjectionResolver func(languageHint string) (lang *Language, highlightQuery string, tokenSourceFactory func(source []byte) TokenSource, ok bool)

// HighlighterInjectionSpec configures nested highlighting for a parent
// language. Query must emit @injection.content and either @injection.language
// or #set! injection.language metadata.
type HighlighterInjectionSpec struct {
	Query           string
	ResolveLanguage HighlighterInjectionResolver
}

var (
	highlighterInjectionMu    sync.RWMutex
	highlighterInjectionSpecs = make(map[string]HighlighterInjectionSpec)
)

// RegisterHighlighterInjection registers nested-highlighting configuration for
// a parent language name (for example "markdown").
func RegisterHighlighterInjection(parentLanguage string, spec HighlighterInjectionSpec) {
	parentLanguage = strings.TrimSpace(parentLanguage)
	if parentLanguage == "" || strings.TrimSpace(spec.Query) == "" || spec.ResolveLanguage == nil {
		return
	}
	highlighterInjectionMu.Lock()
	highlighterInjectionSpecs[parentLanguage] = spec
	highlighterInjectionMu.Unlock()
}

func lookupHighlighterInjection(parentLanguage string) (HighlighterInjectionSpec, bool) {
	highlighterInjectionMu.RLock()
	spec, ok := highlighterInjectionSpecs[parentLanguage]
	highlighterInjectionMu.RUnlock()
	return spec, ok
}

func normalizeInjectionLanguageHint(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && len(s) > 2 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	if strings.HasPrefix(s, "language-") {
		s = strings.TrimPrefix(s, "language-")
	}
	s = strings.TrimPrefix(s, ".")
	parts := strings.Fields(s)
	if len(parts) > 0 {
		s = parts[0]
	}
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '+' || ch == '#' || ch == '.' {
			i++
			continue
		}
		break
	}
	s = strings.TrimSuffix(s[:i], ".")
	return strings.TrimPrefix(strings.TrimSpace(s), ".")
}

package grammars

import (
	"os"
	"strings"
	"sync"
)

var (
	runtimeLanguageSetOnce sync.Once
	runtimeLanguageSet     map[string]struct{}
	runtimeLanguageEnabled bool
)

func languageEnabled(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}
	if !compileTimeLanguageEnabled(name) {
		return false
	}

	set, enabled := runtimeLanguageWhitelist()
	if !enabled {
		return true
	}
	_, ok := set[name]
	return ok
}

func runtimeLanguageWhitelist() (map[string]struct{}, bool) {
	runtimeLanguageSetOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("GOTREESITTER_GRAMMAR_SET"))
		if raw == "" {
			runtimeLanguageSet = nil
			runtimeLanguageEnabled = false
			return
		}
		runtimeLanguageSet = map[string]struct{}{}
		for _, part := range strings.Split(raw, ",") {
			name := strings.TrimSpace(strings.ToLower(part))
			if name == "" {
				continue
			}
			runtimeLanguageSet[name] = struct{}{}
		}
		runtimeLanguageEnabled = len(runtimeLanguageSet) > 0
	})
	return runtimeLanguageSet, runtimeLanguageEnabled
}

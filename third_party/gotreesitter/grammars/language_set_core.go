//go:build grammar_set_core

package grammars

var coreLanguageSet = languageSetFromNames(core100LanguageNames)

func compileTimeLanguageEnabled(name string) bool {
	_, ok := coreLanguageSet[name]
	return ok
}

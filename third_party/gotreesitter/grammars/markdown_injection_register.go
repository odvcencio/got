package grammars

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const markdownFenceInjectionQuery = `
((fenced_code_block
  (info_string
    (language) @injection.language)
  (block_continuation) @injection.start
  (code_fence_content) @injection.content))
`

func init() {
	gotreesitter.RegisterHighlighterInjection("markdown", gotreesitter.HighlighterInjectionSpec{
		Query:           markdownFenceInjectionQuery,
		ResolveLanguage: resolveMarkdownFenceLanguage,
	})
}

func resolveMarkdownFenceLanguage(hint string) (lang *gotreesitter.Language, highlightQuery string, tokenSourceFactory func(source []byte) gotreesitter.TokenSource, ok bool) {
	name := strings.TrimSpace(strings.ToLower(hint))
	if name == "" {
		return nil, "", nil, false
	}

	switch name {
	case "golang":
		name = "go"
	case "js":
		name = "javascript"
	case "ts":
		name = "typescript"
	case "py":
		name = "python"
	case "sh", "shell":
		name = "bash"
	case "yml":
		name = "yaml"
	}

	entry := DetectLanguageByName(name)
	if entry == nil || strings.TrimSpace(entry.HighlightQuery) == "" {
		return nil, "", nil, false
	}

	lang = entry.Language()
	if lang == nil {
		return nil, "", nil, false
	}

	if entry.TokenSourceFactory != nil {
		factory := entry.TokenSourceFactory
		tokenSourceFactory = func(source []byte) gotreesitter.TokenSource {
			return factory(source, lang)
		}
	}

	return lang, entry.HighlightQuery, tokenSourceFactory, true
}

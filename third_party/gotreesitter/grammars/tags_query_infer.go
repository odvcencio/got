package grammars

import (
	"strings"
	"sync"
)

type tagsQueryPattern struct {
	Query   string
	Symbols []string
}

var (
	inferredTagsQueryCache sync.Map // map[string]string
)

var inferredTagsQueryPatterns = []tagsQueryPattern{
	// Common definitions.
	{Query: "(function_declaration (identifier) @name) @definition.function", Symbols: []string{"function_declaration", "identifier"}},
	{Query: "(function_declaration (type_identifier) @name) @definition.function", Symbols: []string{"function_declaration", "type_identifier"}},
	{Query: "(function_definition (identifier) @name) @definition.function", Symbols: []string{"function_definition", "identifier"}},
	{Query: "(function_definition (field_identifier) @name) @definition.function", Symbols: []string{"function_definition", "field_identifier"}},
	{Query: "(function_definition (function_declarator (identifier) @name)) @definition.function", Symbols: []string{"function_definition", "function_declarator", "identifier"}},
	{Query: "(function_definition (function_declarator (field_identifier) @name)) @definition.function", Symbols: []string{"function_definition", "function_declarator", "field_identifier"}},
	{Query: "(method_declaration (identifier) @name) @definition.method", Symbols: []string{"method_declaration", "identifier"}},
	{Query: "(method_declaration (field_identifier) @name) @definition.method", Symbols: []string{"method_declaration", "field_identifier"}},
	{Query: "(method_definition (property_identifier) @name) @definition.method", Symbols: []string{"method_definition", "property_identifier"}},
	{Query: "(method_definition (identifier) @name) @definition.method", Symbols: []string{"method_definition", "identifier"}},
	{Query: "(class_declaration (identifier) @name) @definition.class", Symbols: []string{"class_declaration", "identifier"}},
	{Query: "(class_declaration (type_identifier) @name) @definition.class", Symbols: []string{"class_declaration", "type_identifier"}},
	{Query: "(class_definition (identifier) @name) @definition.class", Symbols: []string{"class_definition", "identifier"}},
	{Query: "(interface_declaration (identifier) @name) @definition.interface", Symbols: []string{"interface_declaration", "identifier"}},
	{Query: "(interface_declaration (type_identifier) @name) @definition.interface", Symbols: []string{"interface_declaration", "type_identifier"}},
	{Query: "(enum_declaration (identifier) @name) @definition.type", Symbols: []string{"enum_declaration", "identifier"}},
	{Query: "(enum_declaration (type_identifier) @name) @definition.type", Symbols: []string{"enum_declaration", "type_identifier"}},
	{Query: "(constructor_declaration (identifier) @name) @definition.constructor", Symbols: []string{"constructor_declaration", "identifier"}},
	{Query: "(type_definition (type_identifier) @name) @definition.type", Symbols: []string{"type_definition", "type_identifier"}},
	{Query: "(type_definition (identifier) @name) @definition.type", Symbols: []string{"type_definition", "identifier"}},
	{Query: "(type_declaration (type_spec (type_identifier) @name)) @definition.type", Symbols: []string{"type_declaration", "type_spec", "type_identifier"}},
	{Query: "(type_declaration (type_alias (type_identifier) @name)) @definition.type", Symbols: []string{"type_declaration", "type_alias", "type_identifier"}},
	{Query: "(function_item (identifier) @name) @definition.function", Symbols: []string{"function_item", "identifier"}},
	{Query: "(function_signature_item (identifier) @name) @definition.function", Symbols: []string{"function_signature_item", "identifier"}},
	{Query: "(struct_item (type_identifier) @name) @definition.type", Symbols: []string{"struct_item", "type_identifier"}},
	{Query: "(enum_item (type_identifier) @name) @definition.type", Symbols: []string{"enum_item", "type_identifier"}},
	{Query: "(trait_item (type_identifier) @name) @definition.type", Symbols: []string{"trait_item", "type_identifier"}},
	{Query: "(class_specifier (type_identifier) @name) @definition.class", Symbols: []string{"class_specifier", "type_identifier"}},
	{Query: "(struct_specifier (type_identifier) @name) @definition.type", Symbols: []string{"struct_specifier", "type_identifier"}},

	// Common call references.
	{Query: "(call_expression (identifier) @name) @reference.call", Symbols: []string{"call_expression", "identifier"}},
	{Query: "(call_expression (field_identifier) @name) @reference.call", Symbols: []string{"call_expression", "field_identifier"}},
	{Query: "(call_expression (property_identifier) @name) @reference.call", Symbols: []string{"call_expression", "property_identifier"}},
	{Query: "(call_expression (member_expression (property_identifier) @name)) @reference.call", Symbols: []string{"call_expression", "member_expression", "property_identifier"}},
	{Query: "(call_expression (selector_expression (field_identifier) @name)) @reference.call", Symbols: []string{"call_expression", "selector_expression", "field_identifier"}},
	{Query: "(call_expression (scoped_identifier (identifier) @name)) @reference.call", Symbols: []string{"call_expression", "scoped_identifier", "identifier"}},
	{Query: "(call (identifier) @name) @reference.call", Symbols: []string{"call", "identifier"}},
	{Query: "(call (attribute (identifier) @name)) @reference.call", Symbols: []string{"call", "attribute", "identifier"}},
	{Query: "(method_invocation (identifier) @name) @reference.call", Symbols: []string{"method_invocation", "identifier"}},
	{Query: "(macro_invocation (identifier) @name) @reference.call", Symbols: []string{"macro_invocation", "identifier"}},
}

func inferredTagsQuery(entry LangEntry) string {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return ""
	}

	if cached, ok := inferredTagsQueryCache.Load(name); ok {
		return cached.(string)
	}

	if entry.Language == nil {
		inferredTagsQueryCache.Store(name, "")
		return ""
	}
	lang := entry.Language()
	if lang == nil {
		inferredTagsQueryCache.Store(name, "")
		return ""
	}

	hasSymbol := func(symbol string) bool {
		_, ok := lang.SymbolByName(symbol)
		return ok
	}

	lines := make([]string, 0, 32)
	seen := make(map[string]struct{}, len(inferredTagsQueryPatterns))
	for _, pattern := range inferredTagsQueryPatterns {
		missing := false
		for _, symbol := range pattern.Symbols {
			if !hasSymbol(symbol) {
				missing = true
				break
			}
		}
		if missing {
			continue
		}
		if _, ok := seen[pattern.Query]; ok {
			continue
		}
		seen[pattern.Query] = struct{}{}
		lines = append(lines, pattern.Query)
	}

	query := strings.Join(lines, "\n")
	inferredTagsQueryCache.Store(name, query)
	return query
}

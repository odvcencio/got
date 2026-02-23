package entity

import (
	"fmt"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Node type classification sets.
var (
	importTypes = map[string]bool{
		"import_declaration":     true,
		"import_statement":       true,
		"import_from_statement":  true,
		"use_declaration":        true,
		"preproc_include":        true,
	}

	declarationTypes = map[string]bool{
		"function_declaration":  true,
		"function_definition":  true,
		"function_item":        true,
		"method_declaration":   true,
		"type_declaration":     true,
		"class_definition":     true,
		"class_declaration":    true,
		"struct_item":          true,
		"enum_item":            true,
		"trait_item":           true,
		"impl_item":            true,
		"interface_declaration": true,
		"const_declaration":    true,
		"var_declaration":      true,
		"decorated_definition": true,
		"export_statement":     true,
		"lexical_declaration":  true,
		"type_spec":            true,
		"short_var_declaration": true,
	}

	preambleTypes = map[string]bool{
		"package_clause":      true,
		"package_declaration": true,
		"module":              true,
	}

	commentTypes = map[string]bool{
		"comment":       true,
		"block_comment": true,
		"line_comment":  true,
	}
)

// nameIdentifierTypes lists node types that represent name identifiers
// found as named children of declarations.
var nameIdentifierTypes = map[string]bool{
	"identifier":         true,
	"type_identifier":    true,
	"field_identifier":   true,
	"package_identifier": true,
	"property_identifier": true,
}

// Extract parses source using tree-sitter and returns an EntityList
// containing structural entities. The critical invariant is that
// concatenating all entity bodies reproduces the original source exactly.
func Extract(filename string, source []byte) (*EntityList, error) {
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filename)
	}

	el := &EntityList{
		Language: entry.Name,
		Path:     filename,
		Source:   source,
	}

	if len(source) == 0 {
		return el, nil
	}

	bt, err := grammars.ParseFile(filename, source)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	defer bt.Release()

	root := bt.RootNode()
	childCount := root.ChildCount()

	if childCount == 0 {
		// Source is non-empty but no children — treat entire source as interstitial.
		e := makeEntity(KindInterstitial, source, 0, uint32(len(source)), 0, 0)
		el.Entities = append(el.Entities, e)
		return el, nil
	}

	// Collect classified nodes from root's immediate children.
	type classifiedNode struct {
		node *gotreesitter.Node
		kind EntityKind
	}
	var nodes []classifiedNode

	for i := 0; i < childCount; i++ {
		child := root.Child(i)
		kind := classifyNode(bt, child)
		nodes = append(nodes, classifiedNode{node: child, kind: kind})
	}

	// Build entities, filling gaps as interstitials.
	var cursor uint32 // tracks current position in source

	for _, cn := range nodes {
		startByte := cn.node.StartByte()
		endByte := cn.node.EndByte()

		// Fill gap before this node as interstitial.
		if startByte > cursor {
			gap := makeEntity(KindInterstitial, source, cursor, startByte, 0, 0)
			el.Entities = append(el.Entities, gap)
		}

		// Create the entity for this node.
		e := makeEntity(cn.kind, source, startByte, endByte, 0, 0)
		e.DeclKind = bt.NodeType(cn.node)

		// Extract name and receiver for declarations.
		if cn.kind == KindDeclaration {
			e.Name, e.Receiver = extractNameAndReceiver(bt, cn.node)
		}

		// Set line numbers from tree-sitter points (0-indexed rows).
		e.StartLine = int(cn.node.StartPoint().Row) + 1
		e.EndLine = int(cn.node.EndPoint().Row) + 1

		el.Entities = append(el.Entities, e)
		cursor = endByte
	}

	// Fill trailing gap as interstitial.
	if cursor < uint32(len(source)) {
		gap := makeEntity(KindInterstitial, source, cursor, uint32(len(source)), 0, 0)
		el.Entities = append(el.Entities, gap)
	}

	// Set PrevEntityKey/NextEntityKey on interstitials.
	setInterstitialNeighborKeys(el)

	return el, nil
}

// classifyNode determines the EntityKind for a root-level tree-sitter node.
func classifyNode(bt *gotreesitter.BoundTree, node *gotreesitter.Node) EntityKind {
	nodeType := bt.NodeType(node)

	if preambleTypes[nodeType] {
		return KindPreamble
	}
	if importTypes[nodeType] {
		return KindImportBlock
	}
	if declarationTypes[nodeType] {
		return KindDeclaration
	}
	if commentTypes[nodeType] {
		return KindInterstitial
	}

	// Fallback: if the node is named and has a child that looks like a name
	// identifier, treat it as a declaration.
	if node.IsNamed() {
		for i := 0; i < node.NamedChildCount(); i++ {
			childType := bt.NodeType(node.NamedChild(i))
			if nameIdentifierTypes[childType] {
				return KindDeclaration
			}
		}
	}

	return KindInterstitial
}

// extractNameAndReceiver extracts the declaration name and optional receiver
// from a tree-sitter node based on its type.
func extractNameAndReceiver(bt *gotreesitter.BoundTree, node *gotreesitter.Node) (name, receiver string) {
	nodeType := bt.NodeType(node)

	switch nodeType {
	case "method_declaration":
		// Go method: children are [parameter_list(receiver), field_identifier(name), parameter_list(params), block]
		return extractGoMethodNameReceiver(bt, node)

	case "function_declaration", "function_definition", "function_item":
		return extractFirstIdentifierName(bt, node), ""

	case "type_declaration":
		// Go: type_declaration -> type_spec -> type_identifier
		return extractGoTypeName(bt, node), ""

	case "class_definition", "class_declaration":
		return extractFirstIdentifierName(bt, node), ""

	case "struct_item", "enum_item", "trait_item", "impl_item":
		return extractFirstIdentifierName(bt, node), ""

	case "interface_declaration":
		return extractFirstIdentifierName(bt, node), ""

	case "var_declaration":
		return extractGoVarConstName(bt, node), ""

	case "const_declaration":
		return extractGoVarConstName(bt, node), ""

	case "decorated_definition":
		// Python: decorated_definition wraps function_definition or class_definition
		return extractDecoratedName(bt, node), ""

	case "export_statement":
		// TypeScript/JS: export_statement wraps a declaration
		return extractExportName(bt, node), ""

	case "lexical_declaration":
		return extractFirstIdentifierName(bt, node), ""

	case "short_var_declaration":
		return extractFirstIdentifierName(bt, node), ""

	default:
		// Generic fallback: look for first identifier-like named child
		return extractFirstIdentifierName(bt, node), ""
	}
}

// extractFirstIdentifierName finds the first named child that looks like an
// identifier and returns its text.
func extractFirstIdentifierName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if nameIdentifierTypes[childType] {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractGoMethodNameReceiver extracts name and receiver from a Go method_declaration.
// Structure: func (receiver) name(params) [result] body
// Named children: [parameter_list(receiver), field_identifier(name), parameter_list(params), ...]
func extractGoMethodNameReceiver(bt *gotreesitter.BoundTree, node *gotreesitter.Node) (name, receiver string) {
	seenFirstParamList := false
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)

		if childType == "parameter_list" && !seenFirstParamList {
			// First parameter_list is the receiver
			receiver = extractReceiverText(bt, child)
			seenFirstParamList = true
			continue
		}
		if childType == "field_identifier" {
			name = bt.NodeText(child)
			break
		}
		if nameIdentifierTypes[childType] {
			name = bt.NodeText(child)
			break
		}
	}
	return
}

// extractReceiverText extracts a clean receiver representation from a parameter_list.
// E.g., "(t T)" -> "t T", "(t *T)" -> "t *T"
func extractReceiverText(bt *gotreesitter.BoundTree, paramList *gotreesitter.Node) string {
	// Look for a parameter_declaration child
	for i := 0; i < paramList.NamedChildCount(); i++ {
		child := paramList.NamedChild(i)
		childType := bt.NodeType(child)
		if childType == "parameter_declaration" {
			return bt.NodeText(child)
		}
	}
	// Fallback: return the inner text (strip parens)
	text := bt.NodeText(paramList)
	if len(text) >= 2 && text[0] == '(' && text[len(text)-1] == ')' {
		return text[1 : len(text)-1]
	}
	return text
}

// extractGoTypeName extracts the name from a Go type_declaration.
// Structure: type_declaration -> type_spec -> type_identifier
func extractGoTypeName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if childType == "type_spec" {
			// type_spec has type_identifier as first named child
			for j := 0; j < child.NamedChildCount(); j++ {
				gc := child.NamedChild(j)
				gcType := bt.NodeType(gc)
				if gcType == "type_identifier" {
					return bt.NodeText(gc)
				}
			}
		}
	}
	return extractFirstIdentifierName(bt, node)
}

// extractGoVarConstName extracts the name from var_declaration or const_declaration.
// Structure: var_declaration -> var_spec -> identifier
// Or: const_declaration -> const_spec -> identifier
func extractGoVarConstName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if childType == "var_spec" || childType == "const_spec" {
			return extractFirstIdentifierName(bt, child)
		}
	}
	return extractFirstIdentifierName(bt, node)
}

// extractDecoratedName extracts the name from a Python decorated_definition.
// It wraps function_definition or class_definition.
func extractDecoratedName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if childType == "function_definition" || childType == "class_definition" {
			return extractFirstIdentifierName(bt, child)
		}
	}
	return extractFirstIdentifierName(bt, node)
}

// extractExportName extracts the name from an export_statement.
// TypeScript/JS: export_statement wraps function_declaration, class_declaration, etc.
func extractExportName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if declarationTypes[childType] {
			n, _ := extractNameAndReceiver(bt, child)
			return n
		}
		// Also check for identifier directly
		if nameIdentifierTypes[childType] {
			return bt.NodeText(child)
		}
	}
	return extractFirstIdentifierName(bt, node)
}

// makeEntity creates an Entity with body bytes, hash, and byte range.
func makeEntity(kind EntityKind, source []byte, startByte, endByte uint32, startLine, endLine int) Entity {
	body := source[startByte:endByte]
	e := Entity{
		Kind:      kind,
		Body:      body,
		StartByte: startByte,
		EndByte:   endByte,
		StartLine: startLine,
		EndLine:   endLine,
	}
	e.ComputeHash()
	return e
}

// setInterstitialNeighborKeys populates PrevEntityKey and NextEntityKey on
// interstitial entities by looking at their non-interstitial neighbors.
func setInterstitialNeighborKeys(el *EntityList) {
	entities := el.Entities
	for i := range entities {
		if entities[i].Kind != KindInterstitial {
			continue
		}

		// Find previous non-interstitial entity
		for j := i - 1; j >= 0; j-- {
			if entities[j].Kind != KindInterstitial {
				entities[i].PrevEntityKey = entities[j].IdentityKey()
				break
			}
		}

		// Find next non-interstitial entity
		for j := i + 1; j < len(entities); j++ {
			if entities[j].Kind != KindInterstitial {
				entities[i].NextEntityKey = entities[j].IdentityKey()
				break
			}
		}
	}
}

package entity

import (
	"fmt"
	"sort"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	classify "github.com/odvcencio/gts-suite/pkg/lang/treesitter"
)

// Aliases for the shared node type classification maps.
var (
	importTypes         = classify.ImportNodeTypes
	declarationTypes    = classify.DeclarationNodeTypes
	preambleTypes       = classify.PreambleNodeTypes
	commentTypes        = classify.CommentNodeTypes
	nameIdentifierTypes = classify.NameIdentifierTypes
)

type classifiedNode struct {
	node     *gotreesitter.Node
	kind     EntityKind
	start    uint32
	end      uint32
	declKind string
	name     string
	receiver string
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
	var nodes []classifiedNode

	for i := 0; i < childCount; i++ {
		child := root.Child(i)
		childType := bt.NodeType(child)
		// Some TypeScript parses surface "class" as a token plus adjacent
		// identifier instead of a single class_declaration node. Synthesize a
		// class declaration entity so class-level identity remains available.
		if childType == "class" && i+1 < childCount {
			next := root.Child(i + 1)
			if next != nil && bt.NodeType(next) == "identifier" {
				nodes = append(nodes, classifiedNode{
					kind:     KindDeclaration,
					start:    child.StartByte(),
					end:      child.EndByte(),
					declKind: "class_declaration",
					name:     bt.NodeText(next),
				})
				continue
			}
		}

		kind := classifyNode(bt, child)
		if kind == KindDeclaration && isContainerDeclaration(bt.NodeType(child)) {
			nested := collectNestedDeclarationNodes(bt, child)
			if len(nested) > 0 {
				sort.Slice(nested, func(i, j int) bool {
					return nested[i].StartByte() < nested[j].StartByte()
				})

				// Preserve class/interface identity by emitting a declaration
				// entity for the container header before flattened members.
				containerStart := child.StartByte()
				containerEnd := nested[0].StartByte()
				if containerEnd < containerStart {
					containerEnd = containerStart
				}
				name, receiver := extractNameAndReceiver(bt, child)
				nodes = append(nodes, classifiedNode{
					kind:     KindDeclaration,
					start:    containerStart,
					end:      containerEnd,
					declKind: bt.NodeType(child),
					name:     name,
					receiver: receiver,
				})

				for _, n := range nested {
					nodes = append(nodes, classifiedNode{node: n, kind: KindDeclaration})
				}
				continue
			}
		}
		if kind == KindInterstitial {
			nested := collectNestedDeclarationNodes(bt, child)
			if len(nested) > 0 {
				for _, n := range nested {
					nodes = append(nodes, classifiedNode{node: n, kind: KindDeclaration})
				}
				continue
			}
		}
		nodes = append(nodes, classifiedNode{node: child, kind: kind})
	}
	sort.Slice(nodes, func(i, j int) bool {
		li, _ := classifiedNodeRange(nodes[i])
		lj, _ := classifiedNodeRange(nodes[j])
		if li == lj {
			_, ei := classifiedNodeRange(nodes[i])
			_, ej := classifiedNodeRange(nodes[j])
			return ei < ej
		}
		return li < lj
	})

	// Build entities, filling gaps as interstitials.
	var cursor uint32 // tracks current position in source

	for _, cn := range nodes {
		startByte, endByte := classifiedNodeRange(cn)
		if endByte < startByte {
			endByte = startByte
		}

		// Fill gap before this node as interstitial.
		if startByte > cursor {
			gap := makeEntity(KindInterstitial, source, cursor, startByte, 0, 0)
			el.Entities = append(el.Entities, gap)
		}

		// Create the entity for this node.
		e := makeEntity(cn.kind, source, startByte, endByte, 0, 0)
		if cn.node != nil {
			e.DeclKind = bt.NodeType(cn.node)
		} else {
			e.DeclKind = cn.declKind
		}

		// Extract name and receiver for declarations.
		if cn.kind == KindDeclaration {
			if cn.node != nil {
				e.Name, e.Receiver = extractNameAndReceiver(bt, cn.node)
			} else {
				e.Name, e.Receiver = cn.name, cn.receiver
			}
			e.Signature = declarationSignature(e.Body)
		}

		// Set line numbers from tree-sitter points (0-indexed rows).
		if cn.node != nil {
			e.StartLine = int(cn.node.StartPoint().Row) + 1
			e.EndLine = int(cn.node.EndPoint().Row) + 1
		} else {
			e.StartLine = lineNumberAtByte(source, startByte)
			e.EndLine = lineNumberAtByte(source, endByte)
		}

		el.Entities = append(el.Entities, e)
		cursor = endByte
	}

	// Fill trailing gap as interstitial.
	if cursor < uint32(len(source)) {
		gap := makeEntity(KindInterstitial, source, cursor, uint32(len(source)), 0, 0)
		el.Entities = append(el.Entities, gap)
	}

	assignIdentityOrdinals(el)
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
	if isDeclarationNode(bt, node) {
		return KindDeclaration
	}
	if commentTypes[nodeType] {
		return KindInterstitial
	}

	return KindInterstitial
}

func isDeclarationNode(bt *gotreesitter.BoundTree, node *gotreesitter.Node) bool {
	nodeType := bt.NodeType(node)
	if declarationTypes[nodeType] {
		return true
	}
	// Some grammars use node names not covered by the shared table.
	if nodeType == "method_definition" {
		return true
	}
	if !node.IsNamed() || !looksLikeDeclarationNodeType(nodeType) {
		return false
	}
	return hasNameIdentifierDescendant(bt, node)
}

func looksLikeDeclarationNodeType(nodeType string) bool {
	return strings.Contains(nodeType, "declaration") ||
		strings.Contains(nodeType, "definition")
}

func hasNameIdentifierDescendant(bt *gotreesitter.BoundTree, node *gotreesitter.Node) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childType := bt.NodeType(child)
		if nameIdentifierTypes[childType] {
			return true
		}
		if hasNameIdentifierDescendant(bt, child) {
			return true
		}
	}
	return false
}

var containerDeclarationNodeTypes = map[string]bool{
	"class_definition":      true,
	"class_declaration":     true,
	"interface_declaration": true,
	"struct_declaration":    true,
	"struct_item":           true,
	"enum_declaration":      true,
	"enum_item":             true,
	"trait_declaration":     true,
	"trait_item":            true,
	"impl_item":             true,
	"object_declaration":    true,
	"record_declaration":    true,
	"protocol_declaration":  true,
}

func isContainerDeclaration(nodeType string) bool {
	return containerDeclarationNodeTypes[nodeType]
}

func collectNestedDeclarationNodes(bt *gotreesitter.BoundTree, node *gotreesitter.Node) []*gotreesitter.Node {
	var out []*gotreesitter.Node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if isDeclarationNode(bt, child) {
			childType := bt.NodeType(child)
			if isContainerDeclaration(childType) {
				nested := collectNestedDeclarationNodes(bt, child)
				if len(nested) > 0 {
					out = append(out, nested...)
					continue
				}
			}
			out = append(out, child)
			continue
		}
		out = append(out, collectNestedDeclarationNodes(bt, child)...)
	}
	return out
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
		// For C/C++ function_definition, the identifier is nested inside
		// a function_declarator child rather than being a direct child.
		name = extractFirstIdentifierName(bt, node)
		if name == "" {
			name = extractDeclaratorName(bt, node)
		}
		return name, ""

	case "type_declaration":
		// Go: type_declaration -> type_spec -> type_identifier
		return extractGoTypeName(bt, node), ""

	case "class_definition", "class_declaration":
		return extractFirstIdentifierName(bt, node), ""

	case "struct_item", "struct_declaration", "enum_item", "enum_declaration", "trait_item", "trait_declaration", "impl_item":
		return extractFirstIdentifierName(bt, node), ""

	case "interface_declaration", "protocol_declaration", "record_declaration", "object_declaration":
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

// extractDeclaratorName looks for a function_declarator (or similar declarator)
// child and extracts the identifier from it. This handles C/C++ where the name
// is nested inside a declarator node rather than being a direct child.
func extractDeclaratorName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	declaratorTypes := map[string]bool{
		"function_declarator": true,
		"init_declarator":     true,
	}
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childType := bt.NodeType(child)
		if declaratorTypes[childType] {
			return extractFirstIdentifierName(bt, child)
		}
	}
	return ""
}

// extractFirstIdentifierName finds the first named child that looks like an
// identifier and returns its text.
func extractFirstIdentifierName(bt *gotreesitter.BoundTree, node *gotreesitter.Node) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childType := bt.NodeType(child)
		if nameIdentifierTypes[childType] {
			return bt.NodeText(child)
		}
		if nested := extractFirstIdentifierName(bt, child); nested != "" {
			return nested
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

func classifiedNodeRange(cn classifiedNode) (uint32, uint32) {
	if cn.node != nil {
		return cn.node.StartByte(), cn.node.EndByte()
	}
	return cn.start, cn.end
}

func declarationSignature(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "{"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return strings.Join(strings.Fields(text), " ")
}

func assignIdentityOrdinals(el *EntityList) {
	counters := make(map[string]int)
	for i := range el.Entities {
		base := identityBaseKey(&el.Entities[i])
		if base == "" {
			continue
		}
		el.Entities[i].Ordinal = counters[base]
		counters[base]++
	}
}

func identityBaseKey(e *Entity) string {
	switch e.Kind {
	case KindPreamble:
		return "preamble"
	case KindImportBlock:
		return "import_block"
	case KindDeclaration:
		return fmt.Sprintf("decl:%s:%s:%s:%s", e.DeclKind, e.Receiver, e.Name, normalizeIdentityText(e.Signature))
	default:
		return ""
	}
}

func lineNumberAtByte(source []byte, bytePos uint32) int {
	if bytePos == 0 {
		return 1
	}
	if int(bytePos) > len(source) {
		bytePos = uint32(len(source))
	}
	line := 1
	for i := uint32(0); i < bytePos; i++ {
		if source[i] == '\n' {
			line++
		}
	}
	return line
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

package entity

import "fmt"

// EntityDisplayName returns a human-readable label for an entity.
// For declarations it includes the short declaration kind and name
// (e.g. "func ProcessOrder"); for other entity kinds it falls back
// to the identity key.
func EntityDisplayName(e *Entity) string {
	if e.Kind == KindDeclaration {
		kind := ShortDeclKind(e.DeclKind)
		if e.Receiver != "" {
			return fmt.Sprintf("%s (%s) %s", kind, e.Receiver, e.Name)
		}
		return fmt.Sprintf("%s %s", kind, e.Name)
	}

	return e.IdentityKey()
}

// ShortDeclKind maps tree-sitter node types to short human-readable labels.
func ShortDeclKind(declKind string) string {
	switch declKind {
	case "function_declaration", "function_definition", "function_item":
		return "func"
	case "method_declaration":
		return "func"
	case "type_declaration", "type_spec":
		return "type"
	case "class_definition", "class_declaration":
		return "class"
	case "struct_item":
		return "struct"
	case "enum_item":
		return "enum"
	case "trait_item":
		return "trait"
	case "impl_item":
		return "impl"
	case "interface_declaration":
		return "interface"
	case "var_declaration":
		return "var"
	case "const_declaration":
		return "const"
	default:
		return declKind
	}
}

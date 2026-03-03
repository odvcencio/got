package entity

import "testing"

func TestEntityDisplayName_FunctionDeclaration(t *testing.T) {
	e := &Entity{
		Kind:     KindDeclaration,
		DeclKind: "function_declaration",
		Name:     "ProcessOrder",
	}
	got := EntityDisplayName(e)
	if got != "func ProcessOrder" {
		t.Errorf("expected %q, got %q", "func ProcessOrder", got)
	}
}

func TestEntityDisplayName_MethodWithReceiver(t *testing.T) {
	e := &Entity{
		Kind:     KindDeclaration,
		DeclKind: "method_declaration",
		Name:     "Process",
		Receiver: "OrderService",
	}
	got := EntityDisplayName(e)
	if got != "func (OrderService) Process" {
		t.Errorf("expected %q, got %q", "func (OrderService) Process", got)
	}
}

func TestEntityDisplayName_TypeDeclaration(t *testing.T) {
	e := &Entity{
		Kind:     KindDeclaration,
		DeclKind: "type_declaration",
		Name:     "Config",
	}
	got := EntityDisplayName(e)
	if got != "type Config" {
		t.Errorf("expected %q, got %q", "type Config", got)
	}
}

func TestEntityDisplayName_ImportBlock(t *testing.T) {
	e := &Entity{
		Kind:    KindImportBlock,
		Ordinal: 0,
	}
	got := EntityDisplayName(e)
	want := "import_block:0"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestEntityDisplayName_Preamble(t *testing.T) {
	e := &Entity{
		Kind:    KindPreamble,
		Ordinal: 0,
	}
	got := EntityDisplayName(e)
	want := "preamble:0"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestEntityDisplayName_ClassDeclaration(t *testing.T) {
	e := &Entity{
		Kind:     KindDeclaration,
		DeclKind: "class_definition",
		Name:     "UserService",
	}
	got := EntityDisplayName(e)
	if got != "class UserService" {
		t.Errorf("expected %q, got %q", "class UserService", got)
	}
}

func TestShortDeclKind(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"function_declaration", "func"},
		{"function_definition", "func"},
		{"function_item", "func"},
		{"method_declaration", "func"},
		{"type_declaration", "type"},
		{"type_spec", "type"},
		{"class_definition", "class"},
		{"class_declaration", "class"},
		{"struct_item", "struct"},
		{"enum_item", "enum"},
		{"trait_item", "trait"},
		{"impl_item", "impl"},
		{"interface_declaration", "interface"},
		{"var_declaration", "var"},
		{"const_declaration", "const"},
		{"unknown_kind", "unknown_kind"},
	}
	for _, tt := range tests {
		got := ShortDeclKind(tt.input)
		if got != tt.want {
			t.Errorf("ShortDeclKind(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

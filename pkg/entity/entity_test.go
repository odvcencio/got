package entity

import (
	"testing"
)

func TestEntityIdentity(t *testing.T) {
	e := Entity{
		Kind:     KindDeclaration,
		Name:     "HandleRequest",
		DeclKind: "function_definition",
		Receiver: "",
		Body:     []byte("func HandleRequest(w http.ResponseWriter, r *http.Request) {\n\treturn\n}"),
	}
	e.ComputeHash()

	if e.BodyHash == "" {
		t.Fatal("expected non-empty body hash")
	}
	if e.IdentityKey() == "" {
		t.Fatal("expected non-empty identity key")
	}

	// Same content, different name = different identity key but same hash
	e2 := Entity{
		Kind:     KindDeclaration,
		Name:     "ServeRequest",
		DeclKind: "function_definition",
		Body:     []byte("func HandleRequest(w http.ResponseWriter, r *http.Request) {\n\treturn\n}"),
	}
	e2.ComputeHash()

	if e.IdentityKey() == e2.IdentityKey() {
		t.Fatal("different names should produce different identity keys")
	}
	if e.BodyHash != e2.BodyHash {
		t.Fatal("same body should produce same hash")
	}
}

func TestEntityKinds(t *testing.T) {
	tests := []struct {
		kind EntityKind
		str  string
	}{
		{KindPreamble, "preamble"},
		{KindImportBlock, "import_block"},
		{KindDeclaration, "declaration"},
		{KindInterstitial, "interstitial"},
	}
	for _, tt := range tests {
		if tt.kind.String() != tt.str {
			t.Errorf("expected %q, got %q", tt.str, tt.kind.String())
		}
	}
}

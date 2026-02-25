package remote

import (
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestValidateHashValid(t *testing.T) {
	valid := object.Hash("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(valid); err != nil {
		t.Fatalf("valid hash rejected: %v", err)
	}
}

func TestValidateHashEmpty(t *testing.T) {
	if err := ValidateHash(""); err == nil {
		t.Fatal("empty hash accepted")
	}
}

func TestValidateHashWrongLength(t *testing.T) {
	if err := ValidateHash("abc123"); err == nil {
		t.Fatal("short hash accepted")
	}
}

func TestValidateHashNonHex(t *testing.T) {
	bad := object.Hash("g1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(bad); err == nil {
		t.Fatal("non-hex hash accepted")
	}
}

func TestValidateHashWhitespace(t *testing.T) {
	if err := ValidateHash("  "); err == nil {
		t.Fatal("whitespace-only hash accepted")
	}
}

func TestParseCapabilities(t *testing.T) {
	caps := ParseCapabilities("pack,zstd,sideband")
	if !caps.Has("pack") {
		t.Fatal("missing pack capability")
	}
	if !caps.Has("zstd") {
		t.Fatal("missing zstd capability")
	}
	if !caps.Has("sideband") {
		t.Fatal("missing sideband capability")
	}
	if caps.Has("nonexistent") {
		t.Fatal("unexpected capability")
	}
}

func TestCapabilitiesIntersect(t *testing.T) {
	a := ParseCapabilities("pack,zstd,sideband")
	b := ParseCapabilities("pack,zstd")
	common := a.Intersect(b)
	if !common.Has("pack") || !common.Has("zstd") {
		t.Fatal("missing intersected capability")
	}
	if common.Has("sideband") {
		t.Fatal("sideband should not be in intersection")
	}
}

func TestCapabilitiesString(t *testing.T) {
	caps := ParseCapabilities("zstd,pack,sideband")
	s := caps.String()
	if s != "pack,sideband,zstd" {
		t.Fatalf("String() = %q, want %q", s, "pack,sideband,zstd")
	}
}

func TestRemoteErrorFormat(t *testing.T) {
	re := &RemoteError{Code: "ref_not_found", Message: "ref not found", Detail: "heads/main"}
	if re.Error() != "ref not found (ref_not_found): heads/main" {
		t.Fatalf("Error() = %q", re.Error())
	}
}

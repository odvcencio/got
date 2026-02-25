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

func TestParseLimitsComplete(t *testing.T) {
	limits := ParseLimits("max_batch=50000,max_payload=67108864,max_object=33554432")
	if limits.MaxBatch != 50000 {
		t.Fatalf("MaxBatch = %d, want 50000", limits.MaxBatch)
	}
	if limits.MaxPayload != 67108864 {
		t.Fatalf("MaxPayload = %d, want 67108864", limits.MaxPayload)
	}
	if limits.MaxObject != 33554432 {
		t.Fatalf("MaxObject = %d, want 33554432", limits.MaxObject)
	}
}

func TestParseLimitsEmpty(t *testing.T) {
	limits := ParseLimits("")
	if limits.MaxBatch != 0 || limits.MaxPayload != 0 || limits.MaxObject != 0 {
		t.Fatalf("expected zero limits for empty string, got %+v", limits)
	}
}

func TestParseLimitsPartial(t *testing.T) {
	limits := ParseLimits("max_batch=1000")
	if limits.MaxBatch != 1000 {
		t.Fatalf("MaxBatch = %d, want 1000", limits.MaxBatch)
	}
	if limits.MaxPayload != 0 {
		t.Fatalf("MaxPayload should be 0 for missing key, got %d", limits.MaxPayload)
	}
}

func TestParseLimitsInvalidValue(t *testing.T) {
	limits := ParseLimits("max_batch=abc,max_payload=100")
	if limits.MaxBatch != 0 {
		t.Fatalf("MaxBatch should be 0 for invalid value, got %d", limits.MaxBatch)
	}
	if limits.MaxPayload != 100 {
		t.Fatalf("MaxPayload = %d, want 100", limits.MaxPayload)
	}
}

func TestParseLimitsNegativeIgnored(t *testing.T) {
	limits := ParseLimits("max_batch=-1")
	if limits.MaxBatch != 0 {
		t.Fatalf("MaxBatch should be 0 for negative value, got %d", limits.MaxBatch)
	}
}

func TestRemoteErrorFormat(t *testing.T) {
	re := &RemoteError{Code: "ref_not_found", Message: "ref not found", Detail: "heads/main"}
	if re.Error() != "ref not found (ref_not_found): heads/main" {
		t.Fatalf("Error() = %q", re.Error())
	}
}

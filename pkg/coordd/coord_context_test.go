package coordd

import (
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestLoadCoordContext_NoAgent(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := LoadCoordContext(r, "", []string{"git", "status"})
	if ctx.Active {
		t.Error("expected Active=false with no agent")
	}
}

func TestLoadCoordContext_NilRepo(t *testing.T) {
	ctx := LoadCoordContext(nil, "some-id", []string{"git", "status"})
	if ctx.Active {
		t.Error("expected Active=false with nil repo")
	}
}

func TestLoadCoordContext_WithAgent(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := coord.New(r, coord.DefaultConfig)
	id, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := LoadCoordContext(r, id, []string{"git", "add", "pkg/foo.go"})
	if !ctx.Active {
		t.Error("expected Active=true")
	}
	if ctx.AgentID != id {
		t.Errorf("AgentID = %q, want %q", ctx.AgentID, id)
	}
	if ctx.AgentName != "cedar" {
		t.Errorf("AgentName = %q, want %q", ctx.AgentName, "cedar")
	}
	if len(ctx.FilesTouched) != 1 || ctx.FilesTouched[0] != "pkg/foo.go" {
		t.Errorf("FilesTouched = %v, want [pkg/foo.go]", ctx.FilesTouched)
	}
}

func TestLoadCoordContext_DetectsConflict(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := coord.New(r, coord.DefaultConfig)

	id1, _ := c.RegisterAgent(coord.AgentInfo{Name: "cedar"})
	_ = c.AcquireClaim(id1, coord.ClaimRequest{
		EntityKey: "file:pkg/foo.go",
		File:      "pkg/foo.go",
		Mode:      coord.ClaimEditing,
	})

	id2, _ := c.RegisterAgent(coord.AgentInfo{Name: "maple"})
	ctx := LoadCoordContext(r, id2, []string{"git", "add", "pkg/foo.go"})
	if len(ctx.ConflictingClaims) != 1 {
		t.Fatalf("ConflictingClaims = %d, want 1", len(ctx.ConflictingClaims))
	}
	if ctx.ConflictingClaims[0].AgentName != "cedar" {
		t.Errorf("conflict agent = %q, want %q", ctx.ConflictingClaims[0].AgentName, "cedar")
	}
}

func TestLoadCoordContext_NoConflictOnOwnClaim(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := coord.New(r, coord.DefaultConfig)

	id, _ := c.RegisterAgent(coord.AgentInfo{Name: "cedar"})
	_ = c.AcquireClaim(id, coord.ClaimRequest{
		EntityKey: "file:pkg/foo.go",
		File:      "pkg/foo.go",
		Mode:      coord.ClaimEditing,
	})

	ctx := LoadCoordContext(r, id, []string{"git", "add", "pkg/foo.go"})
	if len(ctx.ConflictingClaims) != 0 {
		t.Errorf("expected 0 conflicts for own claim, got %d", len(ctx.ConflictingClaims))
	}
}

func TestExtractFilesFromArgv(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want []string
	}{
		{"git add", []string{"git", "add", "a.go", "b.go"}, []string{"a.go", "b.go"}},
		{"git add with flag", []string{"git", "add", "-f", "a.go"}, []string{"a.go"}},
		{"git status", []string{"git", "status"}, nil},
		{"touch", []string{"touch", "new.go"}, []string{"new.go"}},
		{"unknown", []string{"python", "script.py"}, nil},
		{"empty", []string{}, nil},
		{"graft add", []string{"graft", "add", "pkg/foo.go"}, []string{"pkg/foo.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilesFromArgv(tt.argv)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

package repo

import (
	"testing"
)

func TestParseOwnersFile_Empty(t *testing.T) {
	of, err := ParseOwnersFile([]byte(""))
	if err != nil {
		t.Fatalf("ParseOwnersFile(empty): %v", err)
	}
	if len(of.Rules) != 0 {
		t.Errorf("expected 0 rules for empty file, got %d", len(of.Rules))
	}
}

func TestParseOwnersFile_CommentsAndBlanks(t *testing.T) {
	data := []byte("# This is a comment\n\n# Another comment\n   \n")
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 0 {
		t.Errorf("expected 0 rules for comments-only file, got %d", len(of.Rules))
	}
}

func TestParseOwnersFile_EntityPatterns(t *testing.T) {
	data := []byte(`# Entity-level ownership
func:*Handler     @backend-team
type:Config*      @platform
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(of.Rules))
	}

	// Rule 1: func:*Handler
	if of.Rules[0].Pattern != "func:*Handler" {
		t.Errorf("rule[0].Pattern = %q, want %q", of.Rules[0].Pattern, "func:*Handler")
	}
	if !of.Rules[0].IsEntity {
		t.Errorf("rule[0].IsEntity = false, want true")
	}
	if len(of.Rules[0].Owners) != 1 || of.Rules[0].Owners[0] != "@backend-team" {
		t.Errorf("rule[0].Owners = %v, want [@backend-team]", of.Rules[0].Owners)
	}

	// Rule 2: type:Config*
	if of.Rules[1].Pattern != "type:Config*" {
		t.Errorf("rule[1].Pattern = %q, want %q", of.Rules[1].Pattern, "type:Config*")
	}
	if !of.Rules[1].IsEntity {
		t.Errorf("rule[1].IsEntity = false, want true")
	}
	if len(of.Rules[1].Owners) != 1 || of.Rules[1].Owners[0] != "@platform" {
		t.Errorf("rule[1].Owners = %v, want [@platform]", of.Rules[1].Owners)
	}
}

func TestParseOwnersFile_PathPatterns(t *testing.T) {
	data := []byte(`pkg/auth/**       @security-team
*.go              @go-team
docs/             @docs-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(of.Rules))
	}

	for _, rule := range of.Rules {
		if rule.IsEntity {
			t.Errorf("rule %q should not be an entity rule", rule.Pattern)
		}
	}

	if of.Rules[0].Pattern != "pkg/auth/**" {
		t.Errorf("rule[0].Pattern = %q, want %q", of.Rules[0].Pattern, "pkg/auth/**")
	}
	if of.Rules[0].Owners[0] != "@security-team" {
		t.Errorf("rule[0].Owners[0] = %q, want %q", of.Rules[0].Owners[0], "@security-team")
	}
}

func TestParseOwnersFile_MultipleOwners(t *testing.T) {
	data := []byte(`pkg/core/**  @alice @bob @core-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(of.Rules))
	}
	if len(of.Rules[0].Owners) != 3 {
		t.Errorf("expected 3 owners, got %d: %v", len(of.Rules[0].Owners), of.Rules[0].Owners)
	}
}

func TestParseOwnersFile_MixedRules(t *testing.T) {
	data := []byte(`# Mixed entity and path rules
func:*Handler     @backend-team
type:Config*      @platform
pkg/auth/**       @security-team
*.go              @go-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(of.Rules))
	}
	if !of.Rules[0].IsEntity {
		t.Errorf("func:*Handler should be entity rule")
	}
	if !of.Rules[1].IsEntity {
		t.Errorf("type:Config* should be entity rule")
	}
	if of.Rules[2].IsEntity {
		t.Errorf("pkg/auth/** should not be entity rule")
	}
	if of.Rules[3].IsEntity {
		t.Errorf("*.go should not be entity rule")
	}
}

func TestOwnersFor_PathMatch(t *testing.T) {
	data := []byte(`pkg/auth/**       @security-team
*.go              @go-team
docs/             @docs-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	tests := []struct {
		path      string
		entityKey string
		want      []string
	}{
		{"pkg/auth/login.go", "", []string{"@security-team", "@go-team"}},
		{"pkg/auth/oauth/token.go", "", []string{"@security-team", "@go-team"}},
		{"pkg/core/main.go", "", []string{"@go-team"}},
		{"docs/readme.txt", "", []string{"@docs-team"}},
		{"random.txt", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := of.OwnersFor(tt.path, tt.entityKey)
			if len(got) != len(tt.want) {
				t.Errorf("OwnersFor(%q, %q) = %v, want %v", tt.path, tt.entityKey, got, tt.want)
				return
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("OwnersFor(%q, %q)[%d] = %q, want %q", tt.path, tt.entityKey, i, got[i], w)
				}
			}
		})
	}
}

func TestOwnersFor_EntityMatch(t *testing.T) {
	data := []byte(`func:*Handler     @backend-team
type:Config*      @platform
func:Process*     @processing-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	tests := []struct {
		path      string
		entityKey string
		want      []string
	}{
		{"main.go", "func:LoginHandler", []string{"@backend-team"}},
		{"main.go", "func:LogoutHandler", []string{"@backend-team"}},
		{"config.go", "type:ConfigManager", []string{"@platform"}},
		{"config.go", "type:Config", []string{"@platform"}},
		{"main.go", "func:ProcessOrder", []string{"@processing-team"}},
		{"main.go", "type:SomeStruct", nil},
		{"main.go", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.entityKey, func(t *testing.T) {
			got := of.OwnersFor(tt.path, tt.entityKey)
			if len(got) != len(tt.want) {
				t.Errorf("OwnersFor(%q, %q) = %v, want %v", tt.path, tt.entityKey, got, tt.want)
				return
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("OwnersFor(%q, %q)[%d] = %q, want %q", tt.path, tt.entityKey, i, got[i], w)
				}
			}
		})
	}
}

func TestOwnersFor_CombinedPathAndEntity(t *testing.T) {
	data := []byte(`# Both path and entity rules
pkg/auth/**       @security-team
func:*Handler     @backend-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	// File in pkg/auth with a Handler entity should match both rules.
	got := of.OwnersFor("pkg/auth/login.go", "func:LoginHandler")
	if len(got) != 2 {
		t.Fatalf("OwnersFor(auth+handler) = %v, want 2 owners", got)
	}
	if got[0] != "@security-team" {
		t.Errorf("got[0] = %q, want @security-team", got[0])
	}
	if got[1] != "@backend-team" {
		t.Errorf("got[1] = %q, want @backend-team", got[1])
	}
}

func TestOwnersFor_DeduplicatesOwners(t *testing.T) {
	data := []byte(`pkg/auth/**       @team-a
pkg/auth/*.go     @team-a
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	got := of.OwnersFor("pkg/auth/login.go", "")
	// Should deduplicate @team-a.
	if len(got) != 1 {
		t.Errorf("expected 1 deduplicated owner, got %v", got)
	}
}

func TestOwnersFor_EntityPatternExactMatch(t *testing.T) {
	data := []byte(`func:MyFunc       @exact-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	got := of.OwnersFor("main.go", "func:MyFunc")
	if len(got) != 1 || got[0] != "@exact-team" {
		t.Errorf("OwnersFor exact match = %v, want [@exact-team]", got)
	}

	// Should NOT match a different function.
	got = of.OwnersFor("main.go", "func:MyFuncExtra")
	if len(got) != 0 {
		t.Errorf("OwnersFor non-match = %v, want empty", got)
	}
}

func TestReadOwnersFile_FromRepo(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// No .graftowners file: should return empty without error.
	of, err := r.ReadOwnersFile()
	if err != nil {
		t.Fatalf("ReadOwnersFile (no file): %v", err)
	}
	if len(of.Rules) != 0 {
		t.Errorf("expected 0 rules when no file exists, got %d", len(of.Rules))
	}
}

func TestParseOwnersFile_SingleFieldLine(t *testing.T) {
	// A line with only a pattern and no owners should be skipped.
	data := []byte("pkg/auth/**\n")
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}
	if len(of.Rules) != 0 {
		t.Errorf("expected 0 rules for line with no owners, got %d", len(of.Rules))
	}
}

func TestOwnersFor_GlobstarPattern(t *testing.T) {
	data := []byte(`src/**/test_*.go  @test-team
`)
	of, err := ParseOwnersFile(data)
	if err != nil {
		t.Fatalf("ParseOwnersFile: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"src/test_main.go", true},
		{"src/pkg/test_util.go", true},
		{"src/a/b/c/test_deep.go", true},
		{"src/main.go", false},
		{"other/test_main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := of.OwnersFor(tt.path, "")
			if tt.want && len(got) == 0 {
				t.Errorf("OwnersFor(%q) returned no owners, expected @test-team", tt.path)
			}
			if !tt.want && len(got) != 0 {
				t.Errorf("OwnersFor(%q) returned %v, expected no owners", tt.path, got)
			}
		})
	}
}

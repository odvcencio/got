package coord

import (
	"errors"
	"testing"
)

func TestReadWriteRepoConfig(t *testing.T) {
	c := newTestCoordinator(t)

	// Reading without a stored config returns defaults
	cfg, err := c.ReadRepoConfig()
	if err != nil {
		t.Fatalf("ReadRepoConfig (default): %v", err)
	}
	if cfg.ConflictMode != "advisory" {
		t.Errorf("default conflict mode = %q, want advisory", cfg.ConflictMode)
	}

	// Write a config
	newCfg := &RepoCoordConfig{
		ConflictMode:      "hard_block",
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
		NotifyOn:          []string{"breaking"},
		IgnorePatterns:    []string{"vendor/*"},
	}
	if err := c.WriteRepoConfig(newCfg); err != nil {
		t.Fatalf("WriteRepoConfig: %v", err)
	}

	// Read it back
	readCfg, err := c.ReadRepoConfig()
	if err != nil {
		t.Fatalf("ReadRepoConfig (after write): %v", err)
	}
	if readCfg.ConflictMode != "hard_block" {
		t.Errorf("conflict mode = %q, want hard_block", readCfg.ConflictMode)
	}
	if len(readCfg.ProtectedEntities) != 1 {
		t.Fatalf("expected 1 protected entity, got %d", len(readCfg.ProtectedEntities))
	}
	if readCfg.ProtectedEntities[0] != "decl:function_definition::MergeFiles:*" {
		t.Errorf("protected entity = %q", readCfg.ProtectedEntities[0])
	}

	// Overwrite the config
	updatedCfg := &RepoCoordConfig{
		ConflictMode: "soft_block",
	}
	if err := c.WriteRepoConfig(updatedCfg); err != nil {
		t.Fatalf("WriteRepoConfig (update): %v", err)
	}
	readCfg2, err := c.ReadRepoConfig()
	if err != nil {
		t.Fatalf("ReadRepoConfig (after update): %v", err)
	}
	if readCfg2.ConflictMode != "soft_block" {
		t.Errorf("conflict mode after update = %q, want soft_block", readCfg2.ConflictMode)
	}
}

func TestIsEntityProtected(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ConflictMode:      "advisory",
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
	}
	if err := c.WriteRepoConfig(cfg); err != nil {
		t.Fatalf("WriteRepoConfig: %v", err)
	}

	if !c.IsEntityProtected("decl:function_definition::MergeFiles:func MergeFiles():0") {
		t.Error("expected MergeFiles to be protected")
	}
	if c.IsEntityProtected("decl:function_definition::DiffFiles:func DiffFiles():0") {
		t.Error("expected DiffFiles to NOT be protected")
	}
}

func TestIsEntityProtected_MultiplePatterns(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ProtectedEntities: []string{
			"decl:function_definition::MergeFiles:*",
			"decl:function_definition::CommitHandler:*",
		},
	}
	if err := c.WriteRepoConfig(cfg); err != nil {
		t.Fatalf("WriteRepoConfig: %v", err)
	}

	tests := []struct {
		key       string
		protected bool
	}{
		{"decl:function_definition::MergeFiles:func MergeFiles():0", true},
		{"decl:function_definition::CommitHandler:func CommitHandler():0", true},
		{"decl:function_definition::DiffFiles:func DiffFiles():0", false},
	}
	for _, tt := range tests {
		got := c.IsEntityProtected(tt.key)
		if got != tt.protected {
			t.Errorf("IsEntityProtected(%q) = %v, want %v", tt.key, got, tt.protected)
		}
	}
}

func TestIsEntityProtected_NoConfig(t *testing.T) {
	c := newTestCoordinator(t)
	// No config written -- nothing should be protected
	if c.IsEntityProtected("decl:function_definition::Anything:func Anything():0") {
		t.Error("expected nothing protected without config")
	}
}

func TestAcquireClaim_ProtectedEntityHardBlock(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
	}
	if err := c.WriteRepoConfig(cfg); err != nil {
		t.Fatalf("WriteRepoConfig: %v", err)
	}

	id, err := c.RegisterAgent(AgentInfo{Name: "agent", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	err = c.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::MergeFiles:func MergeFiles():0",
		File:      "merge.go",
		Mode:      ClaimEditing,
	})
	if err == nil {
		t.Fatal("expected hard block on protected entity")
	}
	if !errors.Is(err, ErrEntityProtected) {
		t.Errorf("expected ErrEntityProtected, got: %v", err)
	}
}

func TestAcquireClaim_ProtectedEntityWatchAllowed(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
	}
	if err := c.WriteRepoConfig(cfg); err != nil {
		t.Fatalf("WriteRepoConfig: %v", err)
	}

	id, err := c.RegisterAgent(AgentInfo{Name: "watcher", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Watching a protected entity should be allowed
	err = c.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::MergeFiles:func MergeFiles():0",
		File:      "merge.go",
		Mode:      ClaimWatching,
	})
	if err != nil {
		t.Fatalf("expected watching to be allowed on protected entity, got: %v", err)
	}
}

func TestMatchEntityPattern(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		match   bool
	}{
		// Exact match
		{"decl:function_definition::Foo:func Foo():0", "decl:function_definition::Foo:func Foo():0", true},
		// Trailing wildcard
		{"decl:function_definition::Foo:*", "decl:function_definition::Foo:func Foo():0", true},
		{"decl:function_definition::Foo:*", "decl:function_definition::Bar:func Bar():0", false},
		// Wildcard in middle segment
		{"decl:*::Foo:func Foo():0", "decl:function_definition::Foo:func Foo():0", true},
		{"decl:*::Foo:func Foo():0", "decl:type_spec::Foo:func Foo():0", true},
		// No match
		{"decl:function_definition::Baz:*", "decl:function_definition::Foo:func Foo():0", false},
		// Empty pattern
		{"", "", true},
	}

	for _, tt := range tests {
		got := matchEntityPattern(tt.pattern, tt.key)
		if got != tt.match {
			t.Errorf("matchEntityPattern(%q, %q) = %v, want %v", tt.pattern, tt.key, got, tt.match)
		}
	}
}

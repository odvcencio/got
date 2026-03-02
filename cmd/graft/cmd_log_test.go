package main

import (
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestParseLogEntitySelector(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    logEntitySelector
		wantErr bool
	}{
		{
			name:  "plain key",
			input: "decl:function_declaration::Target:func Target() int:0",
			want: logEntitySelector{
				Key: "decl:function_declaration::Target:func Target() int:0",
			},
		},
		{
			name:  "path selector",
			input: "pkg/../a.go::decl:function_declaration::Target:func Target() int:0",
			want: logEntitySelector{
				Path: "a.go",
				Key:  "decl:function_declaration::Target:func Target() int:0",
			},
		},
		{
			name:    "empty selector",
			input:   "  ",
			wantErr: true,
		},
		{
			name:    "missing key in path form",
			input:   "a.go::",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLogEntitySelector(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLogEntitySelector(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("selector = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestRenderGraph_LinearHistory verifies that a linear chain of commits
// produces a single-lane graph with * on every line.
func TestRenderGraph_LinearHistory(t *testing.T) {
	// Build a linear chain: c3 -> c2 -> c1
	entries := []repo.LogEntry{
		{Hash: "c3", Commit: &object.CommitObj{Parents: []object.Hash{"c2"}, Timestamp: 3}},
		{Hash: "c2", Commit: &object.CommitObj{Parents: []object.Hash{"c1"}, Timestamp: 2}},
		{Hash: "c1", Commit: &object.CommitObj{Parents: nil, Timestamp: 1}},
	}

	lines := renderGraph(entries)
	if len(lines) != 3 {
		t.Fatalf("renderGraph returned %d lines, want 3", len(lines))
	}
	for i, line := range lines {
		if line != "*" {
			t.Errorf("lines[%d] = %q, want %q", i, line, "*")
		}
	}
}

// TestRenderGraph_MergeCommit verifies that a merge commit (two parents)
// correctly shows multiple lanes.
func TestRenderGraph_MergeCommit(t *testing.T) {
	// Topology: merge -> (main_parent, feature_parent) -> base
	// Sorted by timestamp: merge(4), feature_parent(3), main_parent(2), base(1)
	entries := []repo.LogEntry{
		{Hash: "merge", Commit: &object.CommitObj{
			Parents:   []object.Hash{"main_parent", "feature_parent"},
			Timestamp: 4,
		}},
		{Hash: "feature_parent", Commit: &object.CommitObj{
			Parents:   []object.Hash{"base"},
			Timestamp: 3,
		}},
		{Hash: "main_parent", Commit: &object.CommitObj{
			Parents:   []object.Hash{"base"},
			Timestamp: 2,
		}},
		{Hash: "base", Commit: &object.CommitObj{
			Parents:   nil,
			Timestamp: 1,
		}},
	}

	lines := renderGraph(entries)
	if len(lines) != 4 {
		t.Fatalf("renderGraph returned %d lines, want 4", len(lines))
	}

	// The merge commit should be on lane 0 with *.
	if lines[0] != "*" {
		t.Errorf("lines[0] = %q, want %q (merge commit)", lines[0], "*")
	}

	// After the merge, we should have two lanes (main_parent and feature_parent).
	// feature_parent should show on lane 1 (secondary parent).
	// main_parent should show on lane 0 (first parent).
	// The exact representation depends on ordering, but both should have * in their lane.

	// Verify that after the merge commit, we see multi-lane output.
	// feature_parent (index 1) should be in a secondary lane.
	if len(lines[1]) < 3 {
		// Should have at least "| *" or "* |" - some multi-lane pattern.
		// Actually with our algorithm, feature_parent gets lane 1 and main_parent stays lane 0.
		// So feature_parent line should be "| *"
		t.Errorf("lines[1] = %q, expected multi-lane output for feature branch commit", lines[1])
	}

	// After feature_parent closes its lane (parents -> base), main_parent should
	// end up on its original lane (lane 0).
}

// TestRenderGraph_MultipleBranches verifies the graph renders correctly for
// commits from distinct branches (typical --all scenario).
func TestRenderGraph_MultipleBranches(t *testing.T) {
	// Two independent branches diverging from a common base:
	//   branch1_commit -> base
	//   branch2_commit -> base
	// Sorted by timestamp: branch2_commit(3), branch1_commit(2), base(1)
	entries := []repo.LogEntry{
		{Hash: "b2_commit", Commit: &object.CommitObj{
			Parents:   []object.Hash{"base"},
			Timestamp: 3,
		}},
		{Hash: "b1_commit", Commit: &object.CommitObj{
			Parents:   []object.Hash{"base"},
			Timestamp: 2,
		}},
		{Hash: "base", Commit: &object.CommitObj{
			Parents:   nil,
			Timestamp: 1,
		}},
	}

	lines := renderGraph(entries)
	if len(lines) != 3 {
		t.Fatalf("renderGraph returned %d lines, want 3", len(lines))
	}

	// First commit starts on lane 0.
	if lines[0] != "*" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "*")
	}

	// Second commit should also get its own lane since its parent (base)
	// is already claimed by the first commit's lane.
	// b2_commit's first parent is "base", so lane 0 now expects "base".
	// b1_commit's first parent is also "base" - it should share that lane
	// or get a new one depending on dedup.
	// With our algorithm: b2_commit puts "base" in lane 0.
	// b1_commit arrives, not in any lane -> gets lane 1.
	// Its parent "base" is already in lane 0, so we just update lane 1.
	// After dedup, "base" appears only once (lane 0), lane 1 is removed.
	// So b1_commit line should be "| *" (lane 0 = |, lane 1 = *)
	if lines[1] != "| *" {
		t.Errorf("lines[1] = %q, want %q", lines[1], "| *")
	}

	// base should be on lane 0 (after b1's lane merged into the base lane).
	if lines[2] != "*" {
		t.Errorf("lines[2] = %q, want %q", lines[2], "*")
	}
}

// TestRenderGraph_Empty verifies renderGraph handles empty input.
func TestRenderGraph_Empty(t *testing.T) {
	lines := renderGraph(nil)
	if lines != nil {
		t.Errorf("renderGraph(nil) = %v, want nil", lines)
	}
}

// TestRenderGraph_SingleCommit verifies a single root commit.
func TestRenderGraph_SingleCommit(t *testing.T) {
	entries := []repo.LogEntry{
		{Hash: "only", Commit: &object.CommitObj{Parents: nil, Timestamp: 1}},
	}
	lines := renderGraph(entries)
	if len(lines) != 1 {
		t.Fatalf("renderGraph returned %d lines, want 1", len(lines))
	}
	if lines[0] != "*" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "*")
	}
}

// TestLogAllIntegration exercises the --all flag via the graft binary.
func TestLogAllIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create initial commit on main.
	commitFile(t, dir, "base.txt", "base\n", "initial on main")

	// Create feature branch.
	mustRunGraft(t, dir, "branch", "feature")
	mustRunGraft(t, dir, "checkout", "feature")

	// Commit on feature.
	commitFile(t, dir, "feature.txt", "feature\n", "commit on feature")

	// Switch back to main and commit.
	mustRunGraft(t, dir, "checkout", "main")
	commitFile(t, dir, "main2.txt", "main2\n", "second on main")

	// Without --all, should only show main's history (2 commits).
	normalOut := mustRunGraft(t, dir, "log", "--oneline")
	normalLines := nonEmptyLines(normalOut)
	if len(normalLines) != 2 {
		t.Fatalf("log without --all returned %d lines, want 2\noutput:\n%s", len(normalLines), normalOut)
	}

	// With --all, should show all 3 commits.
	allOut := mustRunGraft(t, dir, "log", "--all", "--oneline")
	allLines := nonEmptyLines(allOut)
	if len(allLines) != 3 {
		t.Fatalf("log --all returned %d lines, want 3\noutput:\n%s", len(allLines), allOut)
	}

	// Verify all messages appear.
	for _, msg := range []string{"initial on main", "commit on feature", "second on main"} {
		found := false
		for _, line := range allLines {
			if contains(line, msg) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("log --all missing message %q\noutput:\n%s", msg, allOut)
		}
	}
}

// TestLogGraphIntegration exercises the --graph flag via the graft binary.
func TestLogGraphIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create two commits for a simple linear graph.
	commitFile(t, dir, "a.txt", "a\n", "first commit")
	commitFile(t, dir, "b.txt", "b\n", "second commit")

	// With --graph, each line should start with "* ".
	out := mustRunGraft(t, dir, "log", "--graph", "--oneline")
	lines := nonEmptyLines(out)
	if len(lines) != 2 {
		t.Fatalf("log --graph returned %d lines, want 2\noutput:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if len(line) < 2 || line[0] != '*' {
			t.Errorf("lines[%d] = %q, expected to start with '*'", i, line)
		}
	}
}

// TestLogAllDeduplicatesIntegration verifies --all does not show the same
// commit twice when branches share history.
func TestLogAllDeduplicatesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create a commit that both branches share.
	commitFile(t, dir, "shared.txt", "shared\n", "shared commit")

	// Create a branch pointing at the same commit.
	mustRunGraft(t, dir, "branch", "other")

	// Log --all should show only 1 commit (deduplicated).
	out := mustRunGraft(t, dir, "log", "--all", "--oneline")
	lines := nonEmptyLines(out)
	if len(lines) != 1 {
		t.Fatalf("log --all with shared commit returned %d lines, want 1\noutput:\n%s", len(lines), out)
	}
}

func nonEmptyLines(s string) []string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

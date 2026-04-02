package coordd

import (
	"slices"
	"testing"
)

func TestBuildBwrapArgs_OrdersRootBindBeforeDevMount(t *testing.T) {
	args := buildBwrapArgs("/tmp/repo", "/tmp/repo/subdir", []string{"git", "status"}, ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true}), true)

	rootIdx := slices.Index(args, "--ro-bind")
	if rootIdx < 0 || rootIdx+2 >= len(args) || args[rootIdx+1] != "/" || args[rootIdx+2] != "/" {
		t.Fatalf("missing root ro-bind in %v", args)
	}
	devIdx := slices.Index(args, "--dev")
	if devIdx < 0 || devIdx+1 >= len(args) || args[devIdx+1] != "/dev" {
		t.Fatalf("missing /dev mount in %v", args)
	}
	if rootIdx > devIdx {
		t.Fatalf("root ro-bind index %d should come before /dev index %d in %v", rootIdx, devIdx, args)
	}
}

func TestBuildDetachedBwrapArgs_RebindsRepoAfterRootBind(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{Argv: []string{"git", "status"}},
	}
	rootDir := "/tmp/repo"
	args := buildBwrapArgs(rootDir, rootDir, input.Action.Argv, ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true}), false)

	lastRootBind := -1
	lastRepoBind := -1
	for i := 0; i < len(args); i++ {
		if args[i] == "--ro-bind" && i+2 < len(args) && args[i+1] == "/" && args[i+2] == "/" {
			lastRootBind = i
		}
		if args[i] == "--bind" && i+2 < len(args) && args[i+1] == rootDir && args[i+2] == rootDir {
			lastRepoBind = i
		}
	}
	if lastRootBind < 0 || lastRepoBind < 0 {
		t.Fatalf("expected root and repo binds in %v", args)
	}
	if lastRepoBind < lastRootBind {
		t.Fatalf("repo bind index %d should come after root bind index %d in %v", lastRepoBind, lastRootBind, args)
	}
}

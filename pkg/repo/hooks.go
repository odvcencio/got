package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// HookName identifies a client-side hook trigger point.
type HookName string

const (
	// HookPreCommit runs before a commit is created. No arguments.
	HookPreCommit HookName = "pre-commit"

	// HookCommitMsg runs after the commit message is known. Receives the path
	// to a temporary file containing the message; the hook may rewrite the file
	// to modify the message.
	HookCommitMsg HookName = "commit-msg"

	// HookPrePush runs before a push operation.
	HookPrePush HookName = "pre-push"

	// HookPostCheckout runs after a checkout operation.
	HookPostCheckout HookName = "post-checkout"

	// HookPreRebase runs before a rebase operation.
	HookPreRebase HookName = "pre-rebase"

	// HookPostMerge runs after a merge operation.
	HookPostMerge HookName = "post-merge"
)

// RunHook executes the named hook script if it exists and is executable.
//
// Returns nil if the hook does not exist or is not executable.
// Returns an error if the hook exists, is executable, and exits non-zero.
//
// Hook scripts receive GRAFT_DIR and GRAFT_WORK_TREE environment variables
// and run with the working directory set to the repository root.
// Hook stdout and stderr are connected to os.Stdout and os.Stderr.
func (r *Repo) RunHook(name HookName, args ...string) error {
	hookPath := filepath.Join(r.GraftDir, "hooks", string(name))

	info, err := os.Stat(hookPath)
	if err != nil {
		// Hook does not exist — not an error.
		return nil
	}

	// Check if the file is executable (any execute bit set).
	if info.Mode()&0o111 == 0 {
		// Hook exists but is not executable — skip silently.
		return nil
	}

	cmd := exec.Command(hookPath, args...)
	cmd.Dir = r.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"GRAFT_DIR="+r.GraftDir,
		"GRAFT_WORK_TREE="+r.RootDir,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %s: %w", name, err)
	}
	return nil
}

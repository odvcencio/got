package repo

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AddEntityHook is called during Add after entity extraction for each file.
// It receives the relative file path and the identity keys of all entities
// found in that file. The CLI layer can set this hook to integrate with
// coordination (e.g., acquiring claims on changed entities).
//
// If the hook returns an error that implements BlockingAddHookError, staging
// aborts. All other hook errors are treated as warnings and ignored.
type AddEntityHook func(path string, entityKeys []string) error

// BlockingAddHookError marks hook errors that should abort staging.
type BlockingAddHookError interface {
	error
	BlocksAdd() bool
}

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

	// HookPostCommit runs after a commit is created.
	HookPostCommit HookName = "post-commit"

	// HookPostPush runs after a push operation completes.
	HookPostPush HookName = "post-push"
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

// RunHookEntry executes a single HookEntry. For entries with a Run command,
// the command is split and spawned with the JSON payload on stdin. For entries
// with a Type field, the built-in hook handler is invoked instead. For entries
// with a Grep field, structural grep is run (requires a *Repo; use
// RunHookEntryWithRepo instead for grep-enabled hooks).
//
// Environment variables GRAFT_HOOK and GRAFT_REPO_ROOT are set for
// external commands. If entry.Timeout is set (Go duration string), the
// process is killed when the timeout expires.
func RunHookEntry(ctx context.Context, repoRoot string, entry HookEntry, payload []byte) error {
	if entry.Grep != "" {
		// Grep hooks require a Repo for structural matching. Open one
		// from repoRoot so the non-Repo call path still works.
		r, err := Open(repoRoot)
		if err != nil {
			return fmt.Errorf("hook %s.%s: cannot open repo for grep: %w", entry.Point, entry.Name, err)
		}
		return runGrepHook(ctx, r, entry)
	}

	if entry.Type != "" {
		return runBuiltinHook(ctx, repoRoot, entry, payload)
	}

	if entry.Run == "" {
		return fmt.Errorf("hook %s.%s: neither run, type, nor grep specified", entry.Point, entry.Name)
	}

	// Apply timeout if specified.
	if entry.Timeout != "" {
		d, err := time.ParseDuration(entry.Timeout)
		if err != nil {
			return fmt.Errorf("hook %s.%s: invalid timeout %q: %w", entry.Point, entry.Name, entry.Timeout, err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	parts := strings.Fields(entry.Run)
	if len(parts) == 0 {
		return fmt.Errorf("hook %s.%s: empty run command", entry.Point, entry.Name)
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"GRAFT_HOOK="+entry.Point+"."+entry.Name,
		"GRAFT_REPO_ROOT="+repoRoot,
	)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook %s.%s: timed out after %s", entry.Point, entry.Name, entry.Timeout)
		}
		return fmt.Errorf("hook %s.%s: %w", entry.Point, entry.Name, err)
	}
	return nil
}

// RunHooksForPoint runs all the given hooks sequentially. If canAbort is
// true (pre-* hooks), execution stops at the first error. If canAbort is
// false (post-* hooks), errors are logged as warnings and all hooks run.
func RunHooksForPoint(ctx context.Context, repoRoot string, hooks []HookEntry, payload []byte, canAbort bool) error {
	for _, h := range hooks {
		if err := RunHookEntry(ctx, repoRoot, h, payload); err != nil {
			if canAbort {
				return err
			}
			log.Printf("WARNING: hook %s.%s failed: %v", h.Point, h.Name, err)
		}
	}
	return nil
}

// runBuiltinHook dispatches to built-in hook implementations based on the
// entry's Type field.
func runBuiltinHook(ctx context.Context, repoRoot string, entry HookEntry, payload []byte) error {
	switch entry.Type {
	case "mirror":
		return runMirrorHook(ctx, repoRoot, entry)
	default:
		return fmt.Errorf("hook %s.%s: unknown built-in type %q", entry.Point, entry.Name, entry.Type)
	}
}

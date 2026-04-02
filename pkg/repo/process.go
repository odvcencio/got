package repo

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
)

// ExternalProcessSpec describes a spawned external process that should pass
// through repo-level governance before execution.
type ExternalProcessSpec struct {
	Context context.Context
	Dir     string
	Env     []string
	Path    string
	Args    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Label   string
}

// ExternalProcessGuard can inspect or block a process before it is executed.
type ExternalProcessGuard func(spec ExternalProcessSpec) error

// ExternalProcessExecutor can fully handle process execution, including any
// governance, sandboxing, and direct child launch behavior.
type ExternalProcessExecutor func(spec ExternalProcessSpec) error

var (
	externalProcessGuardMu sync.RWMutex
	externalProcessGuard   ExternalProcessGuard
	externalProcessExecMu  sync.RWMutex
	externalProcessExec    ExternalProcessExecutor
)

// SetExternalProcessGuard installs a package-level process guard and returns
// the previous one. Passing nil removes the guard.
func SetExternalProcessGuard(guard ExternalProcessGuard) ExternalProcessGuard {
	externalProcessGuardMu.Lock()
	defer externalProcessGuardMu.Unlock()
	prev := externalProcessGuard
	externalProcessGuard = guard
	return prev
}

func currentExternalProcessGuard() ExternalProcessGuard {
	externalProcessGuardMu.RLock()
	defer externalProcessGuardMu.RUnlock()
	return externalProcessGuard
}

// SetExternalProcessExecutor installs a package-level process executor and
// returns the previous one. Passing nil removes the executor.
func SetExternalProcessExecutor(executor ExternalProcessExecutor) ExternalProcessExecutor {
	externalProcessExecMu.Lock()
	defer externalProcessExecMu.Unlock()
	prev := externalProcessExec
	externalProcessExec = executor
	return prev
}

func currentExternalProcessExecutor() ExternalProcessExecutor {
	externalProcessExecMu.RLock()
	defer externalProcessExecMu.RUnlock()
	return externalProcessExec
}

// RunExternalProcess applies the configured process guard and then executes
// the command directly on the host with the provided stdio, env, and cwd.
func RunExternalProcess(spec ExternalProcessSpec) error {
	if spec.Context == nil {
		spec.Context = context.Background()
	}
	if executor := currentExternalProcessExecutor(); executor != nil {
		return executor(spec)
	}
	if guard := currentExternalProcessGuard(); guard != nil {
		if err := guard(spec); err != nil {
			return err
		}
	}
	return RunExternalProcessDirect(spec)
}

// RunExternalProcessDirect executes the command directly on the host without
// invoking any configured guard or executor.
func RunExternalProcessDirect(spec ExternalProcessSpec) error {
	cmd := exec.CommandContext(spec.Context, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdin = spec.Stdin
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	if len(spec.Env) > 0 {
		cmd.Env = append([]string(nil), spec.Env...)
	} else {
		cmd.Env = os.Environ()
	}
	return cmd.Run()
}

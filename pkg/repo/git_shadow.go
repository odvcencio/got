package repo

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const shadowFailuresLog = "shadow-failures.log"
const shadowBatchSize = 500

// HasGitDir returns true if a .git/ entry (directory or file for linked
// worktrees) exists in the repository root.
func (r *Repo) HasGitDir() bool {
	_, err := os.Lstat(filepath.Join(r.RootDir, ".git"))
	return err == nil
}

// gitPath returns the path to the git binary. It prefers the PATH lookup
// but falls back to /usr/bin/git.
func gitPath() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	return "/usr/bin/git"
}

// gitShadow runs git with the given args via RunExternalProcess, logging
// failures to the shadow-failures log. Label is used for tracing.
func (r *Repo) gitShadow(label string, args ...string) error {
	return r.gitShadowEnv(label, nil, args...)
}

// gitShadowEnv runs git with custom env vars via RunExternalProcess, logging
// failures to the shadow-failures log.
func (r *Repo) gitShadowEnv(label string, env []string, args ...string) error {
	spec := ExternalProcessSpec{
		Dir:    r.RootDir,
		Env:    env,
		Path:   gitPath(),
		Args:   args,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Label:  label,
	}
	if err := RunExternalProcess(spec); err != nil {
		r.logShadowFailure(label, args, err)
		return err
	}
	return nil
}

// logShadowFailure appends a failure entry to .graft/shadow-failures.log.
func (r *Repo) logShadowFailure(label string, args []string, err error) {
	logPath := filepath.Join(r.GraftDir, shadowFailuresLog)
	f, ferr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(f, "%s label=%s args=%v err=%v\n", ts, label, args, err)
}

// HasShadowFailures returns true if the shadow-failures log exists and is
// non-empty.
func (r *Repo) HasShadowFailures() bool {
	logPath := filepath.Join(r.GraftDir, shadowFailuresLog)
	info, err := os.Stat(logPath)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// ClearShadowFailures removes the shadow-failures log file.
func (r *Repo) ClearShadowFailures() {
	os.Remove(filepath.Join(r.GraftDir, shadowFailuresLog))
}

// --- Public shadow API ---
// All methods are no-ops if HasGitDir() returns false.

// GitShadowStage stages the given paths via git add, batched in groups of
// 500 to avoid argument length limits.
func (r *Repo) GitShadowStage(paths []string) {
	if !r.HasGitDir() {
		return
	}
	for i := 0; i < len(paths); i += shadowBatchSize {
		end := i + shadowBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		args := append([]string{"add", "--"}, paths[i:end]...)
		r.gitShadow("git-shadow:stage", args...)
	}
}

// GitShadowCommit creates a git commit with the given message and author.
// If amend is true, --amend is added. The committer is always set to
// graft/graft@noreply via environment variables.
func (r *Repo) GitShadowCommit(message, author string, amend bool) {
	if !r.HasGitDir() {
		return
	}
	args := []string{"commit", "--allow-empty", "-m", message, "--author", author}
	if amend {
		args = append(args, "--amend")
	}
	env := append(os.Environ(),
		"GIT_COMMITTER_NAME=graft",
		"GIT_COMMITTER_EMAIL=graft@noreply",
	)
	r.gitShadowEnv("git-shadow:commit", env, args...)
}

// GitShadowCreateBranch creates a new git branch.
func (r *Repo) GitShadowCreateBranch(name string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:create-branch", "branch", name)
}

// GitShadowDeleteBranch force-deletes a git branch.
func (r *Repo) GitShadowDeleteBranch(name string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:delete-branch", "branch", "-D", name)
}

// GitShadowCheckout checks out the given ref.
func (r *Repo) GitShadowCheckout(ref string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:checkout", "checkout", ref)
}

// GitShadowCreateTag creates a lightweight tag.
func (r *Repo) GitShadowCreateTag(name string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:create-tag", "tag", name)
}

// GitShadowDeleteTag deletes a tag.
func (r *Repo) GitShadowDeleteTag(name string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:delete-tag", "tag", "-d", name)
}

// GitShadowReset runs git reset with the given mode and target. If mode is
// empty, no --<mode> flag is passed.
func (r *Repo) GitShadowReset(mode, target string) {
	if !r.HasGitDir() {
		return
	}
	var args []string
	if mode != "" {
		args = []string{"reset", "--" + mode, target}
	} else {
		args = []string{"reset", target}
	}
	r.gitShadow("git-shadow:reset", args...)
}

// GitShadowResetPaths runs git reset -- <paths>.
func (r *Repo) GitShadowResetPaths(paths []string) {
	if !r.HasGitDir() {
		return
	}
	args := append([]string{"reset", "--"}, paths...)
	r.gitShadow("git-shadow:reset-paths", args...)
}

// GitShadowStash runs git stash <sub> where sub is "push", "pop", or "drop".
func (r *Repo) GitShadowStash(sub string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:stash", "stash", sub)
}

// GitShadowRm runs git rm --cached -- <paths>.
func (r *Repo) GitShadowRm(paths []string) {
	if !r.HasGitDir() {
		return
	}
	args := append([]string{"rm", "--cached", "--"}, paths...)
	r.gitShadow("git-shadow:rm", args...)
}

// GitShadowSyncSnapshot stages all files and creates a snapshot commit.
func (r *Repo) GitShadowSyncSnapshot(message, author string) {
	if !r.HasGitDir() {
		return
	}
	r.gitShadow("git-shadow:sync-stage", "add", "-A", "--", ".")
	env := append(os.Environ(),
		"GIT_COMMITTER_NAME=graft",
		"GIT_COMMITTER_EMAIL=graft@noreply",
	)
	r.gitShadowEnv("git-shadow:sync-commit", env,
		"commit", "--allow-empty", "-m", message, "--author", author,
	)
}

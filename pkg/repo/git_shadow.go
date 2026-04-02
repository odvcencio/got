package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HasGitDir reports whether a colocated .git/ directory exists beside .graft/.
func (r *Repo) HasGitDir() bool {
	gitDir := filepath.Join(r.RootDir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GitShadowCheckout switches the colocated .git/ repository to the given
// branch. If the branch does not exist yet it is created. Errors are
// best-effort and logged to the shadow-failures log.
func (r *Repo) GitShadowCheckout(branch string) {
	if !r.HasGitDir() {
		return
	}
	// Try switching to existing branch first.
	cmd := exec.Command("git", "checkout", branch)
	cmd.Dir = r.RootDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch may not exist; try creating it.
		cmd2 := exec.Command("git", "checkout", "-B", branch)
		cmd2.Dir = r.RootDir
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			r.logShadowFailure("checkout %s: %s: %v", branch, strings.TrimSpace(string(out)), err)
			r.logShadowFailure("checkout -B %s: %s: %v", branch, strings.TrimSpace(string(out2)), err2)
		}
	}
}

// GitShadowSyncSnapshot stages all working-tree files in the colocated .git/
// repository and creates a commit with the given message and author. This
// brings git into exact alignment with graft's tracked content.
func (r *Repo) GitShadowSyncSnapshot(msg, author string) {
	if !r.HasGitDir() {
		return
	}

	// Stage everything.
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = r.RootDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		r.logShadowFailure("add -A: %s: %v", strings.TrimSpace(string(out)), err)
		return
	}

	// Build commit command with author if provided.
	// Git requires author in "Name <email>" format; if the graft author
	// doesn't contain an email part, wrap it so git accepts it.
	args := []string{"commit", "--allow-empty", "-m", msg}
	if author != "" {
		if !strings.Contains(author, "<") {
			author = author + " <graft@local>"
		}
		args = append(args, "--author", author)
	}
	commitCmd := exec.Command("git", args...)
	commitCmd.Dir = r.RootDir
	// Set env to avoid gpg signing issues in automated context.
	commitCmd.Env = append(os.Environ(), "GIT_COMMITTER_NAME=graft", "GIT_COMMITTER_EMAIL=graft@local")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		r.logShadowFailure("commit: %s: %v", strings.TrimSpace(string(out)), err)
	}
}

// ClearShadowFailures removes the shadow-failures.log file if it exists.
func (r *Repo) ClearShadowFailures() {
	logPath := filepath.Join(r.GraftDir, "shadow-failures.log")
	_ = os.Remove(logPath)
}

// logShadowFailure appends a line to .graft/shadow-failures.log.
func (r *Repo) logShadowFailure(format string, args ...interface{}) {
	logPath := filepath.Join(r.GraftDir, "shadow-failures.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", args...)
}

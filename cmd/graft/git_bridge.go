package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func inferRepoNameFromRemote(remoteSpec string) string {
	remoteSpec = strings.TrimSpace(remoteSpec)
	if remoteSpec == "" {
		return "repo"
	}
	if strings.HasPrefix(remoteSpec, "git@") {
		if _, path, ok := strings.Cut(remoteSpec, ":"); ok {
			name := filepath.Base(path)
			name = strings.TrimSuffix(name, ".git")
			if strings.TrimSpace(name) != "" {
				return name
			}
		}
		return "repo"
	}
	if strings.Contains(remoteSpec, "://") {
		trimmed := strings.Trim(strings.TrimSpace(remoteSpec), "/")
		base := filepath.Base(trimmed)
		base = strings.TrimSuffix(base, ".git")
		if strings.TrimSpace(base) != "" {
			return base
		}
	}
	return "repo"
}

func cloneFromGitRemote(cmd *cobra.Command, remoteURL, absDest, remoteName, branch string, bootstrapGot bool) error {
	args := []string{"clone"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(branch))
	}
	args = append(args, remoteURL, absDest)
	if err := runGitStreaming(cmd.Context(), "", cmd.OutOrStdout(), cmd.ErrOrStderr(), args...); err != nil {
		return err
	}
	if !bootstrapGot {
		return nil
	}
	if err := bootstrapGotFromGit(cmd.Context(), absDest, remoteName, remoteURL, cmd.OutOrStdout()); err != nil {
		return err
	}
	return nil
}

func bootstrapGotFromGit(ctx context.Context, dest, remoteName, remoteURL string, out io.Writer) error {
	r, err := repo.Open(dest)
	if err != nil {
		r, err = repo.Init(dest)
		if err != nil {
			return err
		}
	}
	if err := r.SetRemote(remoteName, remoteURL); err != nil {
		return err
	}

	tracked, err := gitTrackedFiles(ctx, dest)
	if err != nil {
		return err
	}
	if len(tracked) == 0 {
		fmt.Fprintf(out, "initialized .graft in %s (empty git repository)\n", dest)
		return nil
	}
	if err := r.Add(tracked); err != nil {
		return err
	}
	author := gitHeadAuthor(ctx, dest)
	if strings.TrimSpace(author) == "" {
		author = "git-import"
	}
	branch := gitCurrentBranch(ctx, dest)
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	msg := "import git HEAD snapshot"
	if branch != "" {
		msg += " (" + branch + ")"
	}
	commitHash, err := r.Commit(msg, author)
	if err != nil {
		return err
	}

	if branch != "main" {
		if err := r.CreateBranch(branch, commitHash); err != nil && !errors.Is(err, repo.ErrBranchAlreadyExists) {
			return err
		}
		if err := r.Checkout(branch); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "initialized .graft from git HEAD (%s)\n", shortHash(commitHash))
	return nil
}

func pushViaGit(cmd *cobra.Command, r *repo.Repo, remoteURL, branch string, force bool) error {
	if err := ensureGitRepository(r.RootDir); err != nil {
		return err
	}
	pushRef, err := resolveGitPushRef(cmd.Context(), r.RootDir, branch)
	if err != nil {
		return err
	}

	if err := syncGitSnapshotFromWorktree(cmd.Context(), r); err != nil {
		return err
	}

	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remoteURL, pushRef)
	return runGitStreaming(cmd.Context(), r.RootDir, cmd.OutOrStdout(), cmd.ErrOrStderr(), args...)
}

func pullViaGit(cmd *cobra.Command, r *repo.Repo, remoteURL, branch string, allowMerge bool, rebase bool) error {
	if err := ensureGitRepository(r.RootDir); err != nil {
		return err
	}
	args := []string{"pull"}
	if rebase {
		args = append(args, "--rebase")
	} else if !allowMerge {
		args = append(args, "--ff-only")
	}
	args = append(args, remoteURL)
	if strings.TrimSpace(branch) != "" {
		args = append(args, strings.TrimSpace(branch))
	}
	if err := runGitStreaming(cmd.Context(), r.RootDir, cmd.OutOrStdout(), cmd.ErrOrStderr(), args...); err != nil {
		return err
	}
	return syncGotSnapshotFromGit(cmd.Context(), r, cmd.OutOrStdout())
}

func ensureGitRepository(root string) error {
	stat, err := os.Stat(filepath.Join(root, ".git"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("git transport requires a .git repository in %s", root)
		}
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("%s/.git is not a directory", root)
	}
	return nil
}

func resolveGitPushRef(ctx context.Context, root, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = gitCurrentBranch(ctx, root)
		if branch == "" {
			return "", fmt.Errorf("cannot infer branch while git HEAD is detached; specify branch")
		}
	}
	if strings.HasPrefix(branch, "refs/tags/") {
		return branch, nil
	}
	if strings.HasPrefix(branch, "refs/heads/") {
		branch = strings.TrimPrefix(branch, "refs/heads/")
	}
	return "HEAD:refs/heads/" + branch, nil
}

func syncGitSnapshotFromWorktree(ctx context.Context, r *repo.Repo) error {
	// .graft is already ignored by bridge bootstrap/config, and passing an
	// explicit exclude pathspec here causes git add to fail once the ignored
	// path is discovered. Stage the worktree root directly and rely on the
	// repo's ignore rules to keep graft metadata out of the mirror commit.
	if err := runGitStreaming(ctx, r.RootDir, io.Discard, io.Discard, "add", "-A", "--", "."); err != nil {
		return err
	}
	if err := runGitStreaming(ctx, r.RootDir, io.Discard, io.Discard, "diff", "--cached", "--quiet"); err == nil {
		return nil
	} else {
		var exitCoder interface{ ExitCode() int }
		if !errors.As(err, &exitCoder) || exitCoder.ExitCode() != 1 {
			return fmt.Errorf("git diff --cached failed: %w", err)
		}
	}

	head, _ := r.ResolveRef("HEAD")
	msg := "sync from graft"
	if strings.TrimSpace(string(head)) != "" {
		msg = fmt.Sprintf("sync from graft %s", shortHash(head))
	}
	commitArgs := []string{
		"-c", "user.name=graft-sync",
		"-c", "user.email=graft-sync@localhost",
		"commit", "-m", msg,
	}
	return runGitStreaming(ctx, r.RootDir, io.Discard, io.Discard, commitArgs...)
}

func syncGotSnapshotFromGit(ctx context.Context, r *repo.Repo, out io.Writer) error {
	tracked, err := gitTrackedFiles(ctx, r.RootDir)
	if err != nil {
		return err
	}
	if len(tracked) > 0 {
		if err := r.Add(tracked); err != nil {
			return err
		}
	}
	status, err := r.Status()
	if err != nil {
		return err
	}
	toRemove := make([]string, 0)
	for _, entry := range status {
		if entry.WorkStatus == repo.StatusDeleted {
			toRemove = append(toRemove, entry.Path)
		}
	}
	if len(toRemove) > 0 {
		if err := r.Remove(toRemove, true); err != nil {
			return err
		}
	}
	status, err = r.Status()
	if err != nil {
		return err
	}
	hasChanges := false
	for _, entry := range status {
		if entry.IndexStatus != repo.StatusClean || entry.WorkStatus != repo.StatusClean {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return nil
	}
	author := gitHeadAuthor(ctx, r.RootDir)
	if strings.TrimSpace(author) == "" {
		author = "git-sync"
	}
	commitHash, err := r.Commit("sync from git pull", author)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "updated .graft snapshot from git (%s)\n", shortHash(commitHash))
	return nil
}

func gitTrackedFiles(ctx context.Context, root string) ([]string, error) {
	out, err := runGitCapture(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(string(p))
		if s == "" {
			continue
		}
		files = append(files, s)
	}
	return files, nil
}

func gitCurrentBranch(ctx context.Context, root string) string {
	out, err := runGitCapture(ctx, root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitHeadAuthor(ctx context.Context, root string) string {
	out, err := runGitCapture(ctx, root, "log", "-1", "--pretty=format:%an <%ae>")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runGitCapture(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGitCaptureWithLabel(ctx, dir, "git-bridge:capture", args...)
}

func runGitStreaming(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	return runGitStreamingWithLabel(ctx, dir, stdout, stderr, "git-bridge:stream", args...)
}

package repo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func runMirrorHook(ctx context.Context, repoRoot string, entry HookEntry) error {
	remoteName := strings.TrimSpace(entry.Remote)
	if remoteName == "" {
		return fmt.Errorf("mirror hook %s: remote is required", entry.Name)
	}

	// Resolve git remote URL
	gitURL, err := resolveGitMirrorRemote(repoRoot, remoteName)
	if err != nil {
		return fmt.Errorf("mirror hook %s: %w", entry.Name, err)
	}

	args := []string{"push"}
	if len(entry.BranchFilter) > 0 {
		args = append(args, gitURL)
		args = append(args, entry.BranchFilter...)
	} else {
		args = append(args, "--mirror", gitURL)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mirror hook %s: git push failed: %s: %w", entry.Name, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func resolveGitMirrorRemote(repoRoot, name string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", name)
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err == nil {
		url := strings.TrimSpace(string(output))
		if url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("git remote %q not found", name)
}

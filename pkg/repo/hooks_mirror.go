package repo

import (
	"bytes"
	"context"
	"fmt"
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

	var output bytes.Buffer
	if err := RunExternalProcess(ExternalProcessSpec{
		Context: ctx,
		Dir:     repoRoot,
		Path:    "git",
		Args:    args,
		Stdout:  &output,
		Stderr:  &output,
		Label:   "hook-mirror:" + entry.Name,
	}); err != nil {
		return fmt.Errorf("mirror hook %s: git push failed: %s: %w", entry.Name, strings.TrimSpace(output.String()), err)
	}
	return nil
}

func resolveGitMirrorRemote(repoRoot, name string) (string, error) {
	var output bytes.Buffer
	err := RunExternalProcess(ExternalProcessSpec{
		Context: context.Background(),
		Dir:     repoRoot,
		Path:    "git",
		Args:    []string{"remote", "get-url", name},
		Stdout:  &output,
		Stderr:  &output,
		Label:   "hook-mirror-resolve:" + name,
	})
	if err == nil {
		url := strings.TrimSpace(output.String())
		if url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("git remote %q not found", name)
}

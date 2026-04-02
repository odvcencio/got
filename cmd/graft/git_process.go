package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
)

func runGitCaptureWithLabel(ctx context.Context, dir, label string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := repo.RunExternalProcess(repo.ExternalProcessSpec{
		Context: ctx,
		Dir:     dir,
		Path:    "git",
		Args:    args,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Label:   label,
	}); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

func runGitStreamingWithLabel(ctx context.Context, dir string, stdout, stderr io.Writer, label string, args ...string) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := repo.RunExternalProcess(repo.ExternalProcessSpec{
		Context: cctx,
		Dir:     dir,
		Path:    "git",
		Args:    append([]string{}, args...),
		Stdout:  stdout,
		Stderr:  stderr,
		Label:   label,
	}); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

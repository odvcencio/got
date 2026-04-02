package gitbridge

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
)

func runGitCapture(rootDir, label string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := repo.RunExternalProcess(repo.ExternalProcessSpec{
		Context: context.Background(),
		Dir:     rootDir,
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

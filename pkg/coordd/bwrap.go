package coordd

import (
	"path/filepath"
	"strings"
)

func buildBwrapArgs(rootDir, cwd string, argv []string, requested RuntimeProfile, dieWithParent bool) []string {
	args := make([]string, 0, len(argv)+16)
	if dieWithParent {
		args = append(args, "--die-with-parent")
	}
	args = append(args,
		"--new-session",
		"--ro-bind", "/", "/",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	)
	if requested.Network == NetworkDeny {
		args = append(args, "--unshare-net")
	}
	if rootDir != "" {
		switch requested.FilesystemScope {
		case FilesystemScopeRepoRW:
			args = append(args, "--bind", rootDir, rootDir)
		case FilesystemScopeRepoRO:
			args = append(args, "--ro-bind", rootDir, rootDir)
		}
	}
	if cwd != "" {
		if rootDir != "" {
			if rel, relErr := filepath.Rel(rootDir, cwd); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				args = append(args, "--chdir", cwd)
			} else {
				args = append(args, "--chdir", rootDir)
			}
		} else {
			args = append(args, "--chdir", cwd)
		}
	}
	args = append(args, "--")
	args = append(args, argv...)
	return args
}

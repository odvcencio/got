package repo

import (
	"os"

	"github.com/odvcencio/got/pkg/object"
)

func modeFromFileInfo(info os.FileInfo) string {
	if info.Mode()&0o111 != 0 {
		return object.TreeModeExecutable
	}
	return object.TreeModeFile
}

func normalizeFileMode(mode string) string {
	if mode == object.TreeModeExecutable {
		return object.TreeModeExecutable
	}
	return object.TreeModeFile
}

func filePermFromMode(mode string) os.FileMode {
	if normalizeFileMode(mode) == object.TreeModeExecutable {
		return 0o755
	}
	return 0o644
}

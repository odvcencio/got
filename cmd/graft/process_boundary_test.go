package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDirectExecIsConfinedToRuntimeLaunchers(t *testing.T) {
	offenders := scanGoFilesForPattern(t,
		[]string{"cmd/graft", "pkg/repo", "pkg/gitbridge", "pkg/coordd"},
		[]string{"exec.Command(", "exec.CommandContext("},
		[]string{
			"pkg/repo/process.go",
			"pkg/coordd/exec.go",
			"pkg/coordd/spawn.go",
		},
	)
	if len(offenders) > 0 {
		t.Fatalf("direct exec usage escaped approved runtime launchers: %s", strings.Join(offenders, ", "))
	}
}

func TestRunExternalProcessDirectUsageIsConfined(t *testing.T) {
	offenders := scanGoFilesForPattern(t,
		[]string{"cmd/graft", "pkg/repo", "pkg/gitbridge", "pkg/coordd"},
		[]string{"RunExternalProcessDirect("},
		[]string{
			"pkg/repo/process.go",
			"cmd/graft/governed_process.go",
		},
	)
	if len(offenders) > 0 {
		t.Fatalf("RunExternalProcessDirect usage escaped approved boundary: %s", strings.Join(offenders, ", "))
	}
}

func scanGoFilesForPattern(t *testing.T, dirs, patterns, allowlist []string) []string {
	t.Helper()

	root := moduleRootFromCaller(t)
	allowed := make(map[string]struct{}, len(allowlist))
	for _, path := range allowlist {
		allowed[filepath.ToSlash(path)] = struct{}{}
	}

	var offenders []string
	for _, dir := range dirs {
		absDir := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			for _, pattern := range patterns {
				if !strings.Contains(string(data), pattern) {
					continue
				}
				if _, ok := allowed[rel]; !ok {
					offenders = append(offenders, rel)
				}
				break
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	slices.Sort(offenders)
	return slices.Compact(offenders)
}

func moduleRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

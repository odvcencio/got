package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/remote"
	"github.com/odvcencio/got/pkg/repo"
)

func looksLikeRemoteURL(s string) bool {
	_, err := remote.ParseEndpoint(s)
	return err == nil
}

func resolveRemoteNameAndURL(r *repo.Repo, remoteArg string) (string, string, error) {
	remoteArg = strings.TrimSpace(remoteArg)
	if remoteArg == "" {
		url, err := r.RemoteURL("origin")
		if err != nil {
			return "", "", fmt.Errorf("remote not configured: %w", err)
		}
		return "origin", url, nil
	}

	if looksLikeRemoteURL(remoteArg) {
		return "origin", remoteArg, nil
	}

	url, err := r.RemoteURL(remoteArg)
	if err != nil {
		return "", "", err
	}
	return remoteArg, url, nil
}

func localRefTips(r *repo.Repo) ([]object.Hash, error) {
	refs, err := r.ListRefs("")
	if err != nil {
		return nil, err
	}
	tips := make([]object.Hash, 0, len(refs))
	for _, h := range refs {
		if strings.TrimSpace(string(h)) != "" {
			tips = append(tips, h)
		}
	}
	return tips, nil
}

func chooseDefaultBranch(remoteRefs map[string]object.Hash) (string, object.Hash, bool) {
	if h, ok := remoteRefs["heads/main"]; ok && strings.TrimSpace(string(h)) != "" {
		return "main", h, true
	}

	branches := make([]string, 0, len(remoteRefs))
	for name := range remoteRefs {
		if strings.HasPrefix(name, "heads/") {
			branches = append(branches, name)
		}
	}
	if len(branches) == 0 {
		return "", "", false
	}
	sort.Strings(branches)

	selected := branches[0]
	return strings.TrimPrefix(selected, "heads/"), remoteRefs[selected], true
}

func ensureEmptyDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("destination path %q is not empty", path)
	}
	return nil
}

func writeSymbolicHead(r *repo.Repo, branch string) error {
	headPath := filepath.Join(r.GotDir, "HEAD")
	content := "ref: refs/heads/" + branch + "\n"
	return os.WriteFile(headPath, []byte(content), 0o644)
}

func remoteTrackingRefName(remoteName, remoteRef string) string {
	return fmt.Sprintf("refs/remotes/%s/%s", remoteName, strings.TrimPrefix(remoteRef, "/"))
}

func ensureCleanWorkingTree(r *repo.Repo) error {
	entries, err := r.Status()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IndexStatus != repo.StatusClean || e.WorkStatus != repo.StatusClean {
			return fmt.Errorf("working tree has uncommitted changes (file %q)", e.Path)
		}
	}
	return nil
}

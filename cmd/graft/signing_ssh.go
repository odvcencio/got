package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
)

func newSSHCommitSigner(keyPath string) (repo.CommitSigner, string, error) {
	resolvedPath, err := resolveSigningKeyPath(keyPath)
	if err != nil {
		return nil, "", err
	}

	signer, err := repo.NewSSHSigner(resolvedPath)
	if err != nil {
		return nil, "", err
	}
	return signer, resolvedPath, nil
}

func resolveSigningKeyPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		expanded, err := expandUserPath(path)
		if err != nil {
			return "", err
		}
		return expanded, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no default SSH private key found in ~/.ssh (id_ed25519, id_ecdsa, id_rsa)")
}

func expandUserPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}

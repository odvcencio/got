package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/repo"
	"golang.org/x/crypto/ssh"
)

const commitSignaturePrefix = "sshsig-v1"

func newSSHCommitSigner(keyPath string) (repo.CommitSigner, string, error) {
	resolvedPath, err := resolveSigningKeyPath(keyPath)
	if err != nil {
		return nil, "", err
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, "", fmt.Errorf("read signing key %q: %w", resolvedPath, err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse signing key %q: %w", resolvedPath, err)
	}

	pub := signer.PublicKey()
	pubB64 := base64.StdEncoding.EncodeToString(pub.Marshal())

	commitSigner := func(payload []byte) (string, error) {
		sig, err := signer.Sign(rand.Reader, payload)
		if err != nil {
			return "", err
		}
		sigB64 := base64.StdEncoding.EncodeToString(sig.Blob)
		return fmt.Sprintf("%s:%s:%s:%s", commitSignaturePrefix, sig.Format, pubB64, sigB64), nil
	}
	return commitSigner, resolvedPath, nil
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

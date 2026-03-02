package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

func TestResolveSSHKeyChoiceFromPath_PublicKeyFallback(t *testing.T) {
	dir := t.TempDir()
	keyBase := filepath.Join(dir, "id_ed25519")
	pubPath := keyBase + ".pub"

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	pubLine := string(ssh.MarshalAuthorizedKey(sshPub))
	if err := os.WriteFile(pubPath, []byte(pubLine), 0o644); err != nil {
		t.Fatalf("write pub: %v", err)
	}

	choice, err := resolveSSHKeyChoiceFromPath(keyBase, "")
	if err != nil {
		t.Fatalf("resolveSSHKeyChoiceFromPath: %v", err)
	}
	if choice.Path != pubPath {
		t.Fatalf("Path = %q, want %q", choice.Path, pubPath)
	}
	if choice.Name != "id_ed25519" {
		t.Fatalf("Name = %q, want id_ed25519", choice.Name)
	}
	if choice.PublicKey == "" {
		t.Fatalf("PublicKey is empty")
	}
	if choice.Fingerprint != ssh.FingerprintSHA256(sshPub) {
		t.Fatalf("Fingerprint mismatch")
	}
}

func TestDiscoverSSHPublicKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}

	pub1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub1, err := ssh.NewPublicKey(pub1)
	if err != nil {
		t.Fatalf("NewPublicKey(1): %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "b_key.pub"), ssh.MarshalAuthorizedKey(sshPub1), 0o644); err != nil {
		t.Fatalf("write b_key.pub: %v", err)
	}

	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub2, err := ssh.NewPublicKey(pub2)
	if err != nil {
		t.Fatalf("NewPublicKey(2): %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "a_key.pub"), ssh.MarshalAuthorizedKey(sshPub2), 0o644); err != nil {
		t.Fatalf("write a_key.pub: %v", err)
	}

	choices, err := discoverSSHPublicKeys()
	if err != nil {
		t.Fatalf("discoverSSHPublicKeys: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("len(choices) = %d, want 2", len(choices))
	}
	if filepath.Base(choices[0].Path) != "a_key.pub" {
		t.Fatalf("choices[0] = %q, want a_key.pub", choices[0].Path)
	}
	if filepath.Base(choices[1].Path) != "b_key.pub" {
		t.Fatalf("choices[1] = %q, want b_key.pub", choices[1].Path)
	}
}

func TestResolveSSHKeyChoiceFromPath_PrivateKeyFallback(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "id_ed25519")

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8,
	})
	if err := os.WriteFile(privatePath, pemData, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	choice, err := resolveSSHKeyChoiceFromPath(privatePath, "agent-key")
	if err != nil {
		t.Fatalf("resolveSSHKeyChoiceFromPath: %v", err)
	}
	if choice.Path != privatePath {
		t.Fatalf("Path = %q, want %q", choice.Path, privatePath)
	}
	if choice.Name != "agent-key" {
		t.Fatalf("Name = %q, want agent-key", choice.Name)
	}
	if choice.PublicKey == "" {
		t.Fatal("PublicKey is empty")
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(choice.PublicKey))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey(choice.PublicKey): %v", err)
	}
	if choice.Fingerprint != ssh.FingerprintSHA256(pub) {
		t.Fatalf("Fingerprint mismatch")
	}
}

func TestMintBootstrapToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/ssh/bootstrap/token" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["ttl_seconds"] != float64(180) {
			t.Fatalf("ttl_seconds = %v, want 180", req["ttl_seconds"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bootstrap_token":"minted-token","expires_at":"2026-02-25T12:00:00Z"}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	resp, err := mintBootstrapToken(cmd, server.URL, "test-token", 180)
	if err != nil {
		t.Fatalf("mintBootstrapToken: %v", err)
	}
	if strings.TrimSpace(resp.BootstrapToken) != "minted-token" {
		t.Fatalf("BootstrapToken = %q, want minted-token", resp.BootstrapToken)
	}
}

func TestMintBootstrapTokenMissingTokenInResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expires_at":"2026-02-25T12:00:00Z"}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	_, err := mintBootstrapToken(cmd, server.URL, "test-token", 0)
	if err == nil {
		t.Fatal("expected error when bootstrap token missing in response")
	}
}

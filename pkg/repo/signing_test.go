package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSigningKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test_key")

	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	// Private key should exist with 0600 permissions.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key permissions = %o, want 0600", perm)
	}

	// Public key should exist with 0644 permissions.
	pubPath := keyPath + ".pub"
	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("stat public key: %v", err)
	}
	if perm := pubInfo.Mode().Perm(); perm != 0o644 {
		t.Errorf("public key permissions = %o, want 0644", perm)
	}

	// Both files should be non-empty.
	privData, _ := os.ReadFile(keyPath)
	if len(privData) == 0 {
		t.Error("private key file is empty")
	}
	pubData, _ := os.ReadFile(pubPath)
	if len(pubData) == 0 {
		t.Error("public key file is empty")
	}
}

func TestSignAndVerifyCommit(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test_key")

	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	payload := []byte("tree abc123\nauthor test\ntimestamp 1234567890\n\ntest commit message\n")

	signature, err := signer(payload)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	if signature == "" {
		t.Fatal("signature is empty")
	}

	// Verify with the public key.
	pubData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}

	if err := VerifySSHSignature(payload, signature, pubData); err != nil {
		t.Fatalf("VerifySSHSignature: %v", err)
	}
}

func TestVerifyBadSignatureFails(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test_key")

	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	payload := []byte("tree abc123\nauthor test\ntimestamp 1234567890\n\ntest commit message\n")
	signature, err := signer(payload)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}

	pubData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}

	// Tamper with payload — verification should fail.
	tamperedPayload := []byte("tree TAMPERED\nauthor test\ntimestamp 1234567890\n\ntest commit message\n")
	if err := VerifySSHSignature(tamperedPayload, signature, pubData); err == nil {
		t.Fatal("expected verification to fail with tampered payload, but it succeeded")
	}

	// Verify with a different key — should also fail.
	key2Path := filepath.Join(dir, "test_key2")
	if err := GenerateSigningKey(key2Path); err != nil {
		t.Fatalf("GenerateSigningKey (key2): %v", err)
	}
	pub2Data, err := os.ReadFile(key2Path + ".pub")
	if err != nil {
		t.Fatalf("read public key 2: %v", err)
	}
	if err := VerifySSHSignature(payload, signature, pub2Data); err == nil {
		t.Fatal("expected verification to fail with wrong key, but it succeeded")
	}
}

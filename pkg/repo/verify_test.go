package repo

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestVerify_ValidSignature generates a signing key, creates a signed commit,
// and verifies the signature succeeds.
func TestVerify_ValidSignature(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Generate a signing key in a temp directory.
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_ed25519")
	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	h, err := r.CommitWithSigner("signed commit", "test-author", signer)
	if err != nil {
		t.Fatalf("CommitWithSigner: %v", err)
	}

	result, err := r.VerifyCommitSignature(h)
	if err != nil {
		t.Fatalf("VerifyCommitSignature: %v", err)
	}

	if result.Unsigned {
		t.Error("expected signed commit, got Unsigned=true")
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, got Valid=false, Error=%q", result.Error)
	}
	if result.Algorithm != "ssh-ed25519" {
		t.Errorf("Algorithm = %q, want %q", result.Algorithm, "ssh-ed25519")
	}
	if result.SignerKey == "" {
		t.Error("SignerKey is empty, expected fingerprint")
	}
	if result.CommitHash != h {
		t.Errorf("CommitHash = %q, want %q", result.CommitHash, h)
	}
}

// TestVerify_UnsignedCommit creates an unsigned commit and verifies it
// returns Unsigned=true.
func TestVerify_UnsignedCommit(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("unsigned commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	result, err := r.VerifyCommitSignature(h)
	if err != nil {
		t.Fatalf("VerifyCommitSignature: %v", err)
	}

	if !result.Unsigned {
		t.Error("expected Unsigned=true for unsigned commit")
	}
	if result.Valid {
		t.Error("expected Valid=false for unsigned commit")
	}
	if result.Algorithm != "" {
		t.Errorf("Algorithm = %q, want empty", result.Algorithm)
	}
	if result.CommitHash != h {
		t.Errorf("CommitHash = %q, want %q", result.CommitHash, h)
	}
}

// TestVerify_AllowedSigners creates an allowed_signers file, creates a signed
// commit, and verifies the commit matches against the allowed signers.
func TestVerify_AllowedSigners(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Generate a signing key.
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_ed25519")
	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	// Read the public key to build the allowed_signers file.
	pubKeyData, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}

	// Create an allowed_signers file.
	signersPath := filepath.Join(keyDir, "allowed_signers")
	// Format: <email> <key-type> <key-data>
	signerLine := "test@example.com " + string(pubKeyData)
	if err := os.WriteFile(signersPath, []byte(signerLine), 0o644); err != nil {
		t.Fatalf("write allowed_signers: %v", err)
	}

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	h, err := r.CommitWithSigner("signed commit", "test-author", signer)
	if err != nil {
		t.Fatalf("CommitWithSigner: %v", err)
	}

	// Load allowed signers.
	signers, err := LoadAllowedSigners(signersPath)
	if err != nil {
		t.Fatalf("LoadAllowedSigners: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}

	// Verify commit against allowed signers.
	commit, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}

	result, err := VerifyCommitAgainstAllowedSigners(commit, signers)
	if err != nil {
		t.Fatalf("VerifyCommitAgainstAllowedSigners: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected Valid=true, got Valid=false, Error=%q", result.Error)
	}
	if result.SignerKey != "test@example.com" {
		t.Errorf("SignerKey = %q, want %q", result.SignerKey, "test@example.com")
	}

	// Now test with a different key not in allowed signers.
	otherKeyPath := filepath.Join(keyDir, "other_ed25519")
	if err := GenerateSigningKey(otherKeyPath); err != nil {
		t.Fatalf("GenerateSigningKey (other): %v", err)
	}
	otherPubData, err := os.ReadFile(otherKeyPath + ".pub")
	if err != nil {
		t.Fatalf("read other public key: %v", err)
	}

	// Create an allowed_signers file with only the other key.
	otherSignersPath := filepath.Join(keyDir, "other_signers")
	otherLine := "other@example.com " + string(otherPubData)
	if err := os.WriteFile(otherSignersPath, []byte(otherLine), 0o644); err != nil {
		t.Fatalf("write other allowed_signers: %v", err)
	}

	otherSigners, err := LoadAllowedSigners(otherSignersPath)
	if err != nil {
		t.Fatalf("LoadAllowedSigners (other): %v", err)
	}

	result2, err := VerifyCommitAgainstAllowedSigners(commit, otherSigners)
	if err != nil {
		t.Fatalf("VerifyCommitAgainstAllowedSigners (other): %v", err)
	}

	if result2.Valid {
		t.Error("expected Valid=false when key not in allowed signers")
	}
	if result2.Error != "signature valid but key not in allowed signers" {
		t.Errorf("Error = %q, want %q", result2.Error, "signature valid but key not in allowed signers")
	}
}

// TestVerify_AllowedSigners_MissingFile verifies that loading a non-existent
// allowed_signers file returns an empty map.
func TestVerify_AllowedSigners_MissingFile(t *testing.T) {
	signers, err := LoadAllowedSigners("/tmp/nonexistent_allowed_signers_file")
	if err != nil {
		t.Fatalf("LoadAllowedSigners: %v", err)
	}
	if len(signers) != 0 {
		t.Errorf("expected empty map, got %d entries", len(signers))
	}
}

// TestVerify_BranchSignatures creates several commits (some signed, some not)
// and verifies the branch walk returns correct results.
func TestVerify_BranchSignatures(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Generate a signing key.
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_ed25519")
	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	// Commit 1: unsigned.
	_, err = r.Commit("unsigned commit 1", "test-author")
	if err != nil {
		t.Fatalf("Commit 1: %v", err)
	}

	// Commit 2: signed.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 2 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.CommitWithSigner("signed commit 2", "test-author", signer)
	if err != nil {
		t.Fatalf("Commit 2: %v", err)
	}

	// Commit 3: unsigned.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 3 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.Commit("unsigned commit 3", "test-author")
	if err != nil {
		t.Fatalf("Commit 3: %v", err)
	}

	// Commit 4: signed.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 4 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.CommitWithSigner("signed commit 4", "test-author", signer)
	if err != nil {
		t.Fatalf("Commit 4: %v", err)
	}

	// Verify branch signatures.
	results, err := r.VerifyBranchSignatures(100)
	if err != nil {
		t.Fatalf("VerifyBranchSignatures: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Results are newest-first: commit4 (signed), commit3 (unsigned),
	// commit2 (signed), commit1 (unsigned).
	if !results[0].Valid {
		t.Errorf("commit 4: expected Valid=true, Error=%q", results[0].Error)
	}
	if results[0].Algorithm != "ssh-ed25519" {
		t.Errorf("commit 4: Algorithm = %q, want %q", results[0].Algorithm, "ssh-ed25519")
	}

	if !results[1].Unsigned {
		t.Error("commit 3: expected Unsigned=true")
	}

	if !results[2].Valid {
		t.Errorf("commit 2: expected Valid=true, Error=%q", results[2].Error)
	}

	if !results[3].Unsigned {
		t.Error("commit 1: expected Unsigned=true")
	}
}

// TestVerify_InvalidSignature verifies that a tampered signature is detected.
func TestVerify_InvalidSignature(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Generate two different keys.
	keyDir := t.TempDir()
	keyPath1 := filepath.Join(keyDir, "key1")
	keyPath2 := filepath.Join(keyDir, "key2")
	if err := GenerateSigningKey(keyPath1); err != nil {
		t.Fatalf("GenerateSigningKey 1: %v", err)
	}
	if err := GenerateSigningKey(keyPath2); err != nil {
		t.Fatalf("GenerateSigningKey 2: %v", err)
	}

	// Read pubkey2 for allowed signers.
	pubKey2Data, err := os.ReadFile(keyPath2 + ".pub")
	if err != nil {
		t.Fatalf("read pubkey2: %v", err)
	}
	_, _ = ssh.ParsePublicKey, pubKey2Data

	// Sign with key1.
	signer, err := NewSSHSigner(keyPath1)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	h, err := r.CommitWithSigner("signed commit", "test-author", signer)
	if err != nil {
		t.Fatalf("CommitWithSigner: %v", err)
	}

	// Self-verification should pass (key in signature matches).
	result, err := r.VerifyCommitSignature(h)
	if err != nil {
		t.Fatalf("VerifyCommitSignature: %v", err)
	}
	if !result.Valid {
		t.Errorf("self-verification should pass, Error=%q", result.Error)
	}
}

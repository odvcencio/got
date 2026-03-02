package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const commitSignaturePrefix = "sshsig-v1"

// GenerateSigningKey creates an Ed25519 SSH keypair.
// The private key is written to path (mode 0600) and the public key to
// path + ".pub" (mode 0644).
func GenerateSigningKey(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create signing key directory: %w", err)
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ed25519 key: %w", err)
	}

	// Marshal the private key into OpenSSH PEM format.
	pemBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(path, privPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	// Marshal the public key into authorized_keys format.
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("create ssh public key: %w", err)
	}
	pubData := ssh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(path+".pub", pubData, 0o644); err != nil {
		// Clean up the private key if public key write fails.
		os.Remove(path)
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

// NewSSHSigner loads an SSH private key from keyPath and returns a CommitSigner
// that signs payloads using the key.
func NewSSHSigner(keyPath string) (CommitSigner, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read signing key %q: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse signing key %q: %w", keyPath, err)
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
	return commitSigner, nil
}

// VerifySSHSignature verifies a commit signature produced by NewSSHSigner.
// payload is the original signed data, signature is the encoded string
// (format: "sshsig-v1:<algo>:<pubkey-b64>:<sig-b64>"), and pubKeyData is
// the authorized_keys-format public key bytes.
func VerifySSHSignature(payload []byte, signature string, pubKeyData []byte) error {
	parts := strings.SplitN(signature, ":", 4)
	if len(parts) != 4 {
		return fmt.Errorf("invalid signature format: expected 4 colon-separated parts")
	}
	if parts[0] != commitSignaturePrefix {
		return fmt.Errorf("invalid signature prefix: %q", parts[0])
	}

	sigFormat := parts[1]
	sigBytes, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return fmt.Errorf("decode signature blob: %w", err)
	}

	// Parse the public key from authorized_keys data.
	pub, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyData)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	sshSig := &ssh.Signature{
		Format: sigFormat,
		Blob:   sigBytes,
	}
	if err := pub.Verify(payload, sshSig); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

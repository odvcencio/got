package repo

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"golang.org/x/crypto/ssh"
)

// VerificationResult holds the result of verifying a single commit's signature.
type VerificationResult struct {
	CommitHash object.Hash
	Valid      bool
	SignerKey  string // public key fingerprint or identifier
	Algorithm  string // signature algorithm (e.g., "ssh-ed25519")
	Error      string // error message if verification failed
	Unsigned   bool   // true if commit has no signature
}

// VerifyCommitSignature verifies a single commit's signature. It reads the
// commit, checks if it has a Signature field. If no signature, the result
// has Unsigned=true. If signed, it extracts the public key from the signature
// string (sshsig-v1:<algo>:<pubkey-b64>:<sig-b64>), rebuilds the signing
// payload, and verifies.
func (r *Repo) VerifyCommitSignature(commitHash object.Hash) (*VerificationResult, error) {
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("verify: read commit %s: %w", commitHash, err)
	}

	result := &VerificationResult{CommitHash: commitHash}

	if commit.Signature == "" {
		result.Unsigned = true
		return result, nil
	}

	// Parse signature: sshsig-v1:<algo>:<pubkey-b64>:<sig-b64>
	parts := strings.SplitN(commit.Signature, ":", 4)
	if len(parts) != 4 {
		result.Error = "invalid signature format: expected 4 colon-separated parts"
		return result, nil
	}
	if parts[0] != commitSignaturePrefix {
		result.Error = fmt.Sprintf("invalid signature prefix: %q", parts[0])
		return result, nil
	}

	algo := parts[1]
	pubKeyB64 := parts[2]
	result.Algorithm = algo

	// Decode public key from base64 (raw SSH marshal format).
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		result.Error = fmt.Sprintf("decode public key: %v", err)
		return result, nil
	}

	// Parse raw SSH public key bytes.
	pubKey, err := ssh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		result.Error = fmt.Sprintf("parse public key: %v", err)
		return result, nil
	}

	// Convert to authorized_keys format for VerifySSHSignature.
	authKeyData := ssh.MarshalAuthorizedKey(pubKey)

	// Compute the signing payload.
	payload := object.CommitSigningPayload(commit)

	// Verify using existing VerifySSHSignature.
	if err := VerifySSHSignature(payload, commit.Signature, authKeyData); err != nil {
		result.Error = fmt.Sprintf("verification failed: %v", err)
		return result, nil
	}

	result.Valid = true
	result.SignerKey = ssh.FingerprintSHA256(pubKey)
	return result, nil
}

// VerifyBranchSignatures walks the current branch history (up to limit
// commits) and verifies each signature. Returns results newest-first.
func (r *Repo) VerifyBranchSignatures(limit int) ([]VerificationResult, error) {
	head, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("verify branch: resolve HEAD: %w", err)
	}

	commits, err := r.Log(head, limit)
	if err != nil {
		return nil, fmt.Errorf("verify branch: log: %w", err)
	}

	// We need the commit hashes. Log returns CommitObj but not hashes directly.
	// Walk again using the hash chain.
	results := make([]VerificationResult, 0, len(commits))
	current := head
	for i := 0; i < len(commits); i++ {
		vr, err := r.VerifyCommitSignature(current)
		if err != nil {
			return nil, err
		}
		results = append(results, *vr)

		if len(commits[i].Parents) == 0 {
			break
		}
		current = commits[i].Parents[0]
	}

	return results, nil
}

// LoadAllowedSigners reads an allowed_signers file. The format is one entry
// per line: "<email> <key-type> <key-data>". Returns a map of email to the
// full authorized_keys line bytes (key-type + " " + key-data). Returns an
// empty map if the file doesn't exist.
func LoadAllowedSigners(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string][]byte), nil
		}
		return nil, fmt.Errorf("load allowed signers: %w", err)
	}
	defer f.Close()

	signers := make(map[string][]byte)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: <email> <key-type> <key-data>
		// We need at least 3 fields.
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 3 {
			return nil, fmt.Errorf("load allowed signers: line %d: expected '<email> <key-type> <key-data>'", lineNum)
		}

		email := fields[0]
		// The authorized_keys format is: <key-type> <key-data>
		authKeysLine := fields[1] + " " + fields[2]
		signers[email] = []byte(authKeysLine)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("load allowed signers: read: %w", err)
	}

	return signers, nil
}

// VerifyCommitAgainstAllowedSigners verifies the commit's signature matches
// one of the allowed signers. It extracts the public key from the signature,
// compares against each allowed signer's key.
func VerifyCommitAgainstAllowedSigners(commit *object.CommitObj, signers map[string][]byte) (*VerificationResult, error) {
	result := &VerificationResult{}

	if commit.Signature == "" {
		result.Unsigned = true
		return result, nil
	}

	// Parse signature.
	parts := strings.SplitN(commit.Signature, ":", 4)
	if len(parts) != 4 {
		result.Error = "invalid signature format: expected 4 colon-separated parts"
		return result, nil
	}
	if parts[0] != commitSignaturePrefix {
		result.Error = fmt.Sprintf("invalid signature prefix: %q", parts[0])
		return result, nil
	}

	algo := parts[1]
	pubKeyB64 := parts[2]
	result.Algorithm = algo

	// Decode the public key embedded in the signature.
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		result.Error = fmt.Sprintf("decode public key: %v", err)
		return result, nil
	}

	sigPubKey, err := ssh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		result.Error = fmt.Sprintf("parse public key: %v", err)
		return result, nil
	}

	// Compute signing payload.
	payload := object.CommitSigningPayload(commit)

	// First verify the signature is internally consistent.
	sigPubKeyAuth := ssh.MarshalAuthorizedKey(sigPubKey)
	if err := VerifySSHSignature(payload, commit.Signature, sigPubKeyAuth); err != nil {
		result.Error = fmt.Sprintf("verification failed: %v", err)
		return result, nil
	}

	// Now check if the signing key matches any allowed signer.
	sigPubKeyMarshaled := sigPubKey.Marshal()
	for email, authKeyLine := range signers {
		allowedPub, _, _, _, err := ssh.ParseAuthorizedKey(authKeyLine)
		if err != nil {
			continue
		}
		if string(allowedPub.Marshal()) == string(sigPubKeyMarshaled) {
			result.Valid = true
			result.SignerKey = email
			return result, nil
		}
	}

	result.Error = "signature valid but key not in allowed signers"
	result.SignerKey = ssh.FingerprintSHA256(sigPubKey)
	return result, nil
}

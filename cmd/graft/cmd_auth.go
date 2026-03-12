package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

type authUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type authTokenResponse struct {
	Token string   `json:"token"`
	User  authUser `json:"user"`
}

type bootstrapTokenMintResponse struct {
	BootstrapToken string `json:"bootstrap_token"`
	ExpiresAt      string `json:"expires_at"`
}

type magicRequestResponse struct {
	Sent      bool   `json:"sent"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type sshChallengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Challenge   string `json:"challenge"`
	Fingerprint string `json:"fingerprint"`
}

type sshPublicKeyChoice struct {
	Name        string
	Path        string
	PublicKey   string
	Fingerprint string
}

type apiErrorResponse struct {
	Error string `json:"error"`
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate graft with Orchard and manage user credentials",
	}
	cmd.AddCommand(newAuthSetupCmd())
	cmd.AddCommand(newAuthSSHLoginCmd())
	cmd.AddCommand(newAuthBootstrapSSHCmd())
	cmd.AddCommand(newAuthRegisterSSHKeyCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}

func newAuthSetupCmd() *cobra.Command {
	var (
		host       string
		email      string
		magicToken string
		sshKeyPath string
		sshKeyName string
		skipSSH    bool
	)
	host = configuredOrchardHost("")

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Sign in with magic link and optionally register an SSH public key",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			email = strings.TrimSpace(email)
			if email == "" {
				if !isInteractiveInput(cmd.InOrStdin()) {
					return fmt.Errorf("email is required (use --email in non-interactive mode)")
				}
				email, err = promptLine(cmd, reader, "Email: ")
				if err != nil {
					return err
				}
			}
			if email == "" {
				return fmt.Errorf("email is required")
			}

			requestResp, err := requestMagicLink(cmd, baseURL, email)
			if err != nil {
				return err
			}

			tokenToVerify := strings.TrimSpace(magicToken)
			if tokenToVerify == "" {
				tokenToVerify = strings.TrimSpace(requestResp.Token)
			}
			if tokenToVerify == "" {
				if !isInteractiveInput(cmd.InOrStdin()) {
					return fmt.Errorf("magic link token not returned; pass --magic-token")
				}
				fmt.Fprintln(cmd.OutOrStdout(), "A magic link token is required to complete login.")
				tokenToVerify, err = promptLine(cmd, reader, "Magic token: ")
				if err != nil {
					return err
				}
			}
			if tokenToVerify == "" {
				return fmt.Errorf("magic token is required")
			}

			authResp, err := verifyMagicToken(cmd, baseURL, tokenToVerify)
			if err != nil {
				return err
			}
			if err := writeAuthConfig(baseURL, authResp.Token, authResp.User); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s on %s\n", authResp.User.Username, baseURL)

			// Auto-generate a signing key if one does not already exist.
			if err := maybeGenerateSigningKey(cmd); err != nil {
				// Non-fatal: warn but continue.
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not set up auto-signing: %v\n", err)
			}

			if skipSSH {
				return nil
			}

			currentToken := configuredTokenForHost(baseURL, authResp.Token)
			if currentToken == "" {
				return fmt.Errorf("cannot register ssh key without auth token")
			}
			return maybeRegisterSSHKeyInteractive(cmd, baseURL, currentToken, sshKeyPath, sshKeyName)
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().StringVar(&email, "email", "", "email for magic-link sign-in")
	cmd.Flags().StringVar(&magicToken, "magic-token", "", "magic-link token (skip prompt)")
	cmd.Flags().StringVar(&sshKeyPath, "ssh-key", "", "SSH public key path to register (defaults to interactive chooser)")
	cmd.Flags().StringVar(&sshKeyName, "ssh-key-name", "", "name for the registered SSH key")
	cmd.Flags().BoolVar(&skipSSH, "skip-ssh", false, "skip SSH key registration step")
	return cmd
}

func newAuthSSHLoginCmd() *cobra.Command {
	var (
		host        string
		username    string
		privateKey  string
		fingerprint string
	)
	host = configuredOrchardHost("")

	cmd := &cobra.Command{
		Use:   "ssh-login",
		Short: "Agent-native login using SSH challenge/response (no browser flow)",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}
			username = strings.TrimSpace(username)
			if username == "" {
				username = configuredUsernameForHost(baseURL)
			}
			if username == "" {
				return fmt.Errorf("username is required (pass --username or configure via auth setup)")
			}

			signer, keyPath, err := loadSSHSigner(privateKey)
			if err != nil {
				return err
			}
			fp := strings.TrimSpace(fingerprint)
			if fp == "" {
				fp = ssh.FingerprintSHA256(signer.PublicKey())
			}

			challenge, err := beginSSHChallenge(cmd, baseURL, username, fp)
			if err != nil {
				return err
			}
			sig, err := signer.Sign(rand.Reader, []byte(challenge.Challenge))
			if err != nil {
				return fmt.Errorf("sign ssh challenge: %w", err)
			}

			authResp, err := verifySSHChallenge(cmd, baseURL, challenge.ChallengeID, sig)
			if err != nil {
				return err
			}
			if err := writeAuthConfig(baseURL, authResp.Token, authResp.User); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s via SSH key %s\n", authResp.User.Username, keyPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().StringVar(&username, "username", "", "username to authenticate as")
	cmd.Flags().StringVar(&privateKey, "ssh-key", "", "SSH private key path (default: ~/.ssh/id_ed25519, id_ecdsa, id_rsa)")
	cmd.Flags().StringVar(&fingerprint, "fingerprint", "", "specific SSH public key fingerprint registered on server")
	return cmd
}

func newAuthBootstrapSSHCmd() *cobra.Command {
	var (
		host           string
		username       string
		email          string
		magicToken     string
		keyPath        string
		keyName        string
		bootstrapToken string
		mintTTL        int
	)
	host = configuredOrchardHost("")

	cmd := &cobra.Command{
		Use:   "bootstrap-ssh",
		Short: "Register first SSH key and return auth token (auto-mints bootstrap token when authenticated)",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}
			reader := bufio.NewReader(cmd.InOrStdin())

			username = strings.TrimSpace(username)
			if username == "" {
				username = configuredUsernameForHost(baseURL)
			}

			token := strings.TrimSpace(bootstrapToken)
			if token == "" {
				token = strings.TrimSpace(os.Getenv("GRAFT_BOOTSTRAP_TOKEN"))
			}
			if token == "" {
				authToken := strings.TrimSpace(configuredTokenForHost(baseURL, ""))
				if authToken != "" {
					mintResp, err := mintBootstrapToken(cmd, baseURL, authToken, mintTTL)
					if err != nil {
						return fmt.Errorf("failed to mint bootstrap token using current auth session: %w", err)
					}
					token = strings.TrimSpace(mintResp.BootstrapToken)
					fmt.Fprintln(cmd.OutOrStdout(), "Minted short-lived bootstrap token from authenticated session.")
				} else {
					// First-time CLI bootstrap fallback: authenticate via magic link first.
					typedEmail := strings.TrimSpace(email)
					if typedEmail == "" {
						if !isInteractiveInput(cmd.InOrStdin()) {
							return fmt.Errorf("email is required for first-time bootstrap (pass --email)")
						}
						typedEmail, err = promptLine(cmd, reader, "Email: ")
						if err != nil {
							return err
						}
					}
					if typedEmail == "" {
						return fmt.Errorf("email is required for first-time bootstrap")
					}
					requestResp, err := requestMagicLink(cmd, baseURL, typedEmail)
					if err != nil {
						return fmt.Errorf("request magic link: %w", err)
					}

					verifyToken := strings.TrimSpace(magicToken)
					if verifyToken == "" {
						verifyToken = strings.TrimSpace(requestResp.Token)
					}
					if verifyToken == "" {
						if !isInteractiveInput(cmd.InOrStdin()) {
							return fmt.Errorf("magic token not returned by server; pass --magic-token from email link")
						}
						fmt.Fprintln(cmd.OutOrStdout(), "Check your email for the magic token.")
						verifyToken, err = promptLine(cmd, reader, "Magic token: ")
						if err != nil {
							return err
						}
					}
					if verifyToken == "" {
						return fmt.Errorf("magic token is required for first-time bootstrap")
					}
					authResp, err := verifyMagicToken(cmd, baseURL, verifyToken)
					if err != nil {
						return fmt.Errorf("verify magic token: %w", err)
					}
					if err := writeAuthConfig(baseURL, authResp.Token, authResp.User); err != nil {
						return err
					}
					authToken = strings.TrimSpace(authResp.Token)
					if username == "" {
						username = strings.TrimSpace(authResp.User.Username)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s on %s\n", authResp.User.Username, baseURL)

					mintResp, err := mintBootstrapToken(cmd, baseURL, authToken, mintTTL)
					if err != nil {
						return fmt.Errorf("mint bootstrap token: %w", err)
					}
					token = strings.TrimSpace(mintResp.BootstrapToken)
					fmt.Fprintln(cmd.OutOrStdout(), "Minted short-lived bootstrap token from authenticated session.")
				}
			}
			if token == "" {
				return fmt.Errorf("bootstrap token is required (pass --bootstrap-token, set GRAFT_BOOTSTRAP_TOKEN, or authenticate first via `graft auth setup`)")
			}
			if username == "" {
				return fmt.Errorf("username is required (pass --username or configure via auth setup)")
			}

			keyArg := strings.TrimSpace(keyPath)
			if keyArg == "" {
				defaultKey, err := resolveSigningKeyPath("")
				if err != nil {
					return fmt.Errorf("resolve default ssh key: %w", err)
				}
				keyArg = defaultKey
			}
			choice, err := resolveSSHKeyChoiceFromPath(keyArg, keyName)
			if err != nil {
				return err
			}

			authResp, err := bootstrapSSHRegister(cmd, baseURL, token, username, choice.Name, choice.PublicKey)
			if err != nil {
				return err
			}
			if err := writeAuthConfig(baseURL, authResp.Token, authResp.User); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Bootstrapped SSH auth for %s on %s with key %s\n", authResp.User.Username, baseURL, choice.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().StringVar(&username, "username", "", "username to bootstrap")
	cmd.Flags().StringVar(&email, "email", "", "email for first-time magic-link fallback when no auth token exists")
	cmd.Flags().StringVar(&magicToken, "magic-token", "", "magic-link token (used when server does not return token inline)")
	cmd.Flags().StringVar(&keyPath, "ssh-key", "", "SSH key path (.pub preferred; private key also accepted)")
	cmd.Flags().StringVar(&keyName, "name", "", "name for registered SSH key")
	cmd.Flags().StringVar(&bootstrapToken, "bootstrap-token", "", "bootstrap token for first-key registration")
	cmd.Flags().IntVar(&mintTTL, "mint-ttl", 300, "requested TTL seconds when minting bootstrap token with current auth session")
	return cmd
}

func newAuthRegisterSSHKeyCmd() *cobra.Command {
	var (
		host      string
		keyPath   string
		keyName   string
		tokenFlag string
	)
	host = configuredOrchardHost("")

	cmd := &cobra.Command{
		Use:   "register-key",
		Short: "Register an SSH public key on Orchard for the current account",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}
			token := configuredTokenForHost(baseURL, tokenFlag)
			if token == "" {
				return fmt.Errorf("auth token not found; run `graft auth setup` or set GRAFT_TOKEN")
			}
			if strings.TrimSpace(keyPath) != "" {
				choice, err := resolveSSHKeyChoiceFromPath(keyPath, keyName)
				if err != nil {
					return err
				}
				return registerSSHKey(cmd, baseURL, token, choice.Name, choice.PublicKey)
			}
			return maybeRegisterSSHKeyInteractive(cmd, baseURL, token, "", keyName)
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "auth token (defaults to GRAFT_TOKEN or ~/.graftconfig token)")
	cmd.Flags().StringVar(&keyPath, "ssh-key", "", "SSH public key path (defaults to interactive chooser)")
	cmd.Flags().StringVar(&keyName, "name", "", "name for the registered SSH key")
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	host := configuredOrchardHost("")
	var all bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current graft auth configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadUserConfig()
			path, err := userconfig.Path()
			if err != nil {
				return err
			}
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}
			for _, line := range formatAuthStatusLines(cfg, path, baseURL, all) {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().BoolVar(&all, "all", false, "show auth state for all configured Orchard hosts")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	host := configuredOrchardHost("")
	var all bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear stored auth token from ~/.graftconfig",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadUserConfig()
			if all {
				clearAllStoredAuthTokens(cfg)
				if err := userconfig.Save(cfg); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Cleared stored auth tokens for all Orchard hosts.")
				return nil
			}
			baseURL, err := normalizeBaseURL(configuredOrchardHost(host), defaultOrchardBaseURL)
			if err != nil {
				return err
			}
			profile := cfg.OrchardProfile(baseURL)
			profile.Token = ""
			cfg.SetOrchardProfile(baseURL, profile)
			if strings.TrimSpace(cfg.DefaultOrchardURL()) == strings.TrimSpace(baseURL) {
				cfg.Token = ""
			}
			if err := userconfig.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cleared stored auth token for %s.\n", baseURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", host, "Orchard base URL (default: --host, GRAFT_ORCHARD_URL, ~/.graftconfig, or https://orchard.dev)")
	cmd.Flags().BoolVar(&all, "all", false, "clear stored auth tokens for all configured Orchard hosts")
	return cmd
}

func formatAuthStatusLines(cfg *userconfig.Config, path, selectedHost string, includeAll bool) []string {
	lines := []string{"config: " + path}
	if !includeAll {
		profile := cfg.OrchardProfile(selectedHost)
		lines = append(lines, "host: "+selectedHost)
		if strings.TrimSpace(profile.Username) != "" {
			lines = append(lines, "username: "+profile.Username)
		}
		if strings.TrimSpace(profile.Owner) != "" {
			lines = append(lines, "owner: "+profile.Owner)
		}
		if strings.TrimSpace(profile.Token) != "" {
			lines = append(lines, "token: set")
		} else {
			lines = append(lines, "token: not set")
		}
		if known := knownAuthHosts(cfg, selectedHost); len(known) > 1 {
			lines = append(lines, fmt.Sprintf("known orchard hosts: %d (use --all to list)", len(known)))
		}
		return lines
	}

	for _, host := range knownAuthHosts(cfg, selectedHost) {
		profile := cfg.OrchardProfile(host)
		labels := make([]string, 0, 3)
		if host == cfg.DefaultOrchardURL() {
			labels = append(labels, "default")
		}
		if host == selectedHost {
			labels = append(labels, "selected")
		}
		if strings.TrimSpace(profile.Token) != "" {
			labels = append(labels, "token:set")
		} else {
			labels = append(labels, "token:not set")
		}
		line := "host: " + host
		if len(labels) > 0 {
			line += " (" + strings.Join(labels, ", ") + ")"
		}
		lines = append(lines, line)
		if strings.TrimSpace(profile.Username) != "" {
			lines = append(lines, "username: "+profile.Username)
		}
		if strings.TrimSpace(profile.Owner) != "" {
			lines = append(lines, "owner: "+profile.Owner)
		}
	}
	return lines
}

func knownAuthHosts(cfg *userconfig.Config, selectedHost string) []string {
	if cfg == nil {
		if strings.TrimSpace(selectedHost) == "" {
			return nil
		}
		return []string{selectedHost}
	}

	seen := make(map[string]struct{})
	hosts := make([]string, 0, len(cfg.OrchardProfileHosts())+2)
	addHost := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}

	addHost(cfg.DefaultOrchardURL())
	addHost(selectedHost)
	for _, host := range cfg.OrchardProfileHosts() {
		addHost(host)
	}
	sort.Strings(hosts)
	return hosts
}

func clearAllStoredAuthTokens(cfg *userconfig.Config) {
	if cfg == nil {
		return
	}
	cfg.Token = ""
	for _, host := range cfg.OrchardProfileHosts() {
		profile := cfg.OrchardProfile(host)
		profile.Token = ""
		cfg.SetOrchardProfile(host, profile)
	}
}

func maybeRegisterSSHKeyInteractive(cmd *cobra.Command, baseURL, token, sshKeyPath, sshKeyName string) error {
	if strings.TrimSpace(sshKeyPath) != "" {
		choice, err := resolveSSHKeyChoiceFromPath(sshKeyPath, sshKeyName)
		if err != nil {
			return err
		}
		return registerSSHKey(cmd, baseURL, token, choice.Name, choice.PublicKey)
	}

	if !isInteractiveInput(cmd.InOrStdin()) {
		fmt.Fprintln(cmd.OutOrStdout(), "Skipping SSH key registration (non-interactive input).")
		return nil
	}

	choices, err := discoverSSHPublicKeys()
	if err != nil {
		return err
	}
	if len(choices) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No SSH public keys found in ~/.ssh (*.pub).")
		return nil
	}

	reader := bufio.NewReader(cmd.InOrStdin())
	yes, err := promptYesNo(cmd, reader, "Register an SSH key now? [Y/n]: ", true)
	if err != nil {
		return err
	}
	if !yes {
		return nil
	}

	selected, err := promptSSHKeyChoice(cmd, reader, choices)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sshKeyName) != "" {
		selected.Name = strings.TrimSpace(sshKeyName)
	}
	return registerSSHKey(cmd, baseURL, token, selected.Name, selected.PublicKey)
}

func requestMagicLink(cmd *cobra.Command, baseURL, email string) (*magicRequestResponse, error) {
	var out magicRequestResponse
	err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/magic/request", "", map[string]any{
		"email": email,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func verifyMagicToken(cmd *cobra.Command, baseURL, token string) (*authTokenResponse, error) {
	var out authTokenResponse
	err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/magic/verify", "", map[string]any{
		"token": token,
	}, &out)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Token) == "" {
		return nil, fmt.Errorf("magic verify response did not include auth token")
	}
	return &out, nil
}

func beginSSHChallenge(cmd *cobra.Command, baseURL, username, fingerprint string) (*sshChallengeResponse, error) {
	payload := map[string]any{
		"username": username,
	}
	if strings.TrimSpace(fingerprint) != "" {
		payload["fingerprint"] = strings.TrimSpace(fingerprint)
	}
	var out sshChallengeResponse
	if err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/ssh/challenge", "", payload, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.ChallengeID) == "" || strings.TrimSpace(out.Challenge) == "" {
		return nil, fmt.Errorf("ssh challenge response missing required fields")
	}
	return &out, nil
}

func verifySSHChallenge(cmd *cobra.Command, baseURL, challengeID string, sig *ssh.Signature) (*authTokenResponse, error) {
	var out authTokenResponse
	err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/ssh/verify", "", map[string]any{
		"challenge_id":     challengeID,
		"signature":        base64.StdEncoding.EncodeToString(sig.Blob),
		"signature_format": sig.Format,
	}, &out)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Token) == "" {
		return nil, fmt.Errorf("ssh verify response did not include auth token")
	}
	return &out, nil
}

func bootstrapSSHRegister(cmd *cobra.Command, baseURL, bootstrapToken, username, keyName, publicKey string) (*authTokenResponse, error) {
	var out authTokenResponse
	err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/ssh/bootstrap", "", map[string]any{
		"username":        strings.TrimSpace(username),
		"name":            strings.TrimSpace(keyName),
		"public_key":      strings.TrimSpace(publicKey),
		"bootstrap_token": strings.TrimSpace(bootstrapToken),
	}, &out)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Token) == "" {
		return nil, fmt.Errorf("bootstrap response did not include auth token")
	}
	return &out, nil
}

func mintBootstrapToken(cmd *cobra.Command, baseURL, authToken string, ttlSeconds int) (*bootstrapTokenMintResponse, error) {
	var out bootstrapTokenMintResponse
	payload := map[string]any{}
	if ttlSeconds > 0 {
		payload["ttl_seconds"] = ttlSeconds
	}
	err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/auth/ssh/bootstrap/token", strings.TrimSpace(authToken), payload, &out)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.BootstrapToken) == "" {
		return nil, fmt.Errorf("bootstrap token mint response missing token")
	}
	return &out, nil
}

func registerSSHKey(cmd *cobra.Command, baseURL, token, name, publicKey string) error {
	name = strings.TrimSpace(name)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" {
		return fmt.Errorf("ssh key name is required")
	}
	if publicKey == "" {
		return fmt.Errorf("ssh public key is required")
	}
	if err := doJSONRequest(cmd, http.MethodPost, baseURL+"/api/v1/user/ssh-keys", token, map[string]any{
		"name":       name,
		"public_key": publicKey,
	}, nil); err != nil {
		return err
	}

	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Registered SSH key %q\n", name)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Registered SSH key %q (%s)\n", name, ssh.FingerprintSHA256(pub))
	return nil
}

func writeAuthConfig(baseURL, token string, user authUser) error {
	cfg := loadUserConfig()
	profile := cfg.OrchardProfile(baseURL)
	profile.Token = strings.TrimSpace(token)
	if username := strings.TrimSpace(user.Username); username != "" {
		profile.Username = username
		if strings.TrimSpace(profile.Owner) == "" {
			profile.Owner = username
		}
	}
	cfg.OrchardURL = cfg.SetOrchardProfile(baseURL, profile)
	cfg.Token = profile.Token
	cfg.Username = profile.Username
	cfg.Owner = profile.Owner
	return userconfig.Save(cfg)
}

func doJSONRequest(cmd *cobra.Command, method, endpoint, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(cmd.Context(), method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		var parsed apiErrorResponse
		if json.Unmarshal(respBody, &parsed) == nil && strings.TrimSpace(parsed.Error) != "" {
			msg = strings.TrimSpace(parsed.Error)
		}
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s %s failed (%d): %s", method, endpoint, resp.StatusCode, msg)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func discoverSSHPublicKeys() ([]sshPublicKeyChoice, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", sshDir, err)
	}

	choices := make([]sshPublicKeyChoice, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".pub") {
			continue
		}
		fullPath := filepath.Join(sshDir, name)
		choice, err := resolveSSHKeyChoiceFromPath(fullPath, "")
		if err != nil {
			continue
		}
		choices = append(choices, choice)
	}
	sort.Slice(choices, func(i, j int) bool {
		return choices[i].Path < choices[j].Path
	})
	return choices, nil
}

func resolveSSHKeyChoiceFromPath(inputPath, keyName string) (sshPublicKeyChoice, error) {
	path, err := expandUserPath(strings.TrimSpace(inputPath))
	if err != nil {
		return sshPublicKeyChoice{}, err
	}
	if strings.TrimSpace(path) == "" {
		return sshPublicKeyChoice{}, fmt.Errorf("ssh key path is empty")
	}

	tryPaths := []string{path}
	if !strings.HasSuffix(path, ".pub") {
		tryPaths = append(tryPaths, path+".pub")
	}

	var raw []byte
	var resolved string
	for _, candidate := range tryPaths {
		data, err := os.ReadFile(candidate)
		if err == nil {
			raw = data
			resolved = candidate
			break
		}
	}
	if len(raw) == 0 {
		privateSigner, err := parsePrivateSSHSigner(path)
		if err != nil {
			return sshPublicKeyChoice{}, fmt.Errorf("failed to read SSH key from %q", inputPath)
		}
		return makeSSHChoiceFromPublicKey(privateSigner.PublicKey(), path, keyName), nil
	}

	line := strings.TrimSpace(string(raw))
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		if resolved == path && !strings.HasSuffix(strings.ToLower(path), ".pub") {
			privateSigner, privateErr := parsePrivateSSHSigner(path)
			if privateErr == nil {
				return makeSSHChoiceFromPublicKey(privateSigner.PublicKey(), path, keyName), nil
			}
		}
		return sshPublicKeyChoice{}, fmt.Errorf("parse SSH public key %q: %w", resolved, err)
	}

	name := strings.TrimSpace(keyName)
	if name == "" {
		base := filepath.Base(resolved)
		name = strings.TrimSuffix(base, ".pub")
	}
	return sshPublicKeyChoice{
		Name:        name,
		Path:        resolved,
		PublicKey:   line,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}

func parsePrivateSSHSigner(path string) (ssh.Signer, error) {
	privateRaw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(privateRaw)
}

func makeSSHChoiceFromPublicKey(pub ssh.PublicKey, path, keyName string) sshPublicKeyChoice {
	name := strings.TrimSpace(keyName)
	if name == "" {
		name = filepath.Base(path)
	}
	return sshPublicKeyChoice{
		Name:        name,
		Path:        path,
		PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		Fingerprint: ssh.FingerprintSHA256(pub),
	}
}

func promptSSHKeyChoice(cmd *cobra.Command, reader *bufio.Reader, choices []sshPublicKeyChoice) (*sshPublicKeyChoice, error) {
	fmt.Fprintln(cmd.OutOrStdout(), "Available SSH public keys:")
	for i := range choices {
		c := choices[i]
		fmt.Fprintf(cmd.OutOrStdout(), "  [%d] %s  %s\n      %s\n", i+1, c.Name, c.Fingerprint, c.Path)
	}
	for {
		line, err := promptLine(cmd, reader, fmt.Sprintf("Select key [1-%d]: ", len(choices)))
		if err != nil {
			return nil, err
		}
		idx, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || idx < 1 || idx > len(choices) {
			fmt.Fprintln(cmd.OutOrStdout(), "Invalid selection.")
			continue
		}
		selected := choices[idx-1]
		return &selected, nil
	}
}

func promptYesNo(cmd *cobra.Command, reader *bufio.Reader, prompt string, defaultYes bool) (bool, error) {
	for {
		line, err := promptLine(cmd, reader, prompt)
		if err != nil {
			return false, err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			return defaultYes, nil
		}
		if line == "y" || line == "yes" {
			return true, nil
		}
		if line == "n" || line == "no" {
			return false, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Please answer yes or no.")
	}
}

func promptLine(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func isInteractiveInput(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func maybeGenerateSigningKey(cmd *cobra.Command) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	graftDir := filepath.Join(home, ".graft")
	keyPath := filepath.Join(graftDir, "signing_key")

	// If the key already exists, just ensure config points to it.
	if _, err := os.Stat(keyPath); err == nil {
		cfg := loadUserConfig()
		if cfg.SigningKeyPath == "" || !cfg.AutoSign {
			cfg.SigningKeyPath = keyPath
			cfg.AutoSign = true
			if err := userconfig.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Auto-signing enabled (key: %s)\n", keyPath)
		return nil
	}

	if err := repo.GenerateSigningKey(keyPath); err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}

	cfg := loadUserConfig()
	cfg.SigningKeyPath = keyPath
	cfg.AutoSign = true
	if err := userconfig.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Generated signing key: %s\n", keyPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Auto-signing enabled for future commits.\n")
	return nil
}

func loadSSHSigner(path string) (ssh.Signer, string, error) {
	keyPath, err := resolveSigningKeyPath(path)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, "", fmt.Errorf("read ssh private key %q: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse ssh private key %q: %w", keyPath, err)
	}
	return signer, keyPath, nil
}

// Package userconfig manages user-level graft configuration stored in
// ~/.config/graft/.
package userconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	currentConfigVersion = 1
	configFileName       = ".graftconfig"
)

// Config stores user-wide graft settings and credentials.
// Environment variables still take precedence over these values.
type OrchardProfile struct {
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

type Config struct {
	Version         int                       `json:"version"`
	Name            string                    `json:"name,omitempty"`
	Email           string                    `json:"email,omitempty"`
	OrchardURL      string                    `json:"orchard_url,omitempty"`
	Token           string                    `json:"token,omitempty"`
	Username        string                    `json:"username,omitempty"`
	Owner           string                    `json:"owner,omitempty"`
	OrchardProfiles map[string]OrchardProfile `json:"orchard_profiles,omitempty"`
	SigningKeyPath  string                    `json:"signing_key_path,omitempty"`
	AutoSign        bool                      `json:"auto_sign,omitempty"`
	AIProvider      string                    `json:"ai_provider,omitempty"` // "claude" (default) or future providers
	AIAPIKey        string                    `json:"ai_api_key,omitempty"`  // API key for AI provider
	AIModel         string                    `json:"ai_model,omitempty"`    // model override (e.g. "claude-opus-4-20250514")
}

// Load reads ~/.graftconfig. Missing file returns an empty config.
func Load() (*Config, error) {
	path, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Version: currentConfigVersion}, nil
		}
		return nil, fmt.Errorf("read user config: %w", err)
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("read user config: unmarshal: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

// Save atomically writes ~/.graftconfig with mode 0600.
func Save(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("user config is nil")
	}
	cfgCopy := *cfg
	cfgCopy.normalize()

	target, err := path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".graftconfig-*")
	if err != nil {
		return fmt.Errorf("write user config: tmpfile: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write user config: chmod: %w", err)
	}

	data, err := json.MarshalIndent(&cfgCopy, "", "  ")
	if err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write user config: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write user config: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write user config: close: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write user config: rename: %w", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return fmt.Errorf("write user config: chmod final: %w", err)
	}
	return nil
}

// Path returns the absolute path for ~/.graftconfig.
func Path() (string, error) {
	return path()
}

// DefaultOrchardURL returns the configured default Orchard base URL.
// If no explicit default is set and exactly one Orchard profile exists,
// that profile's host becomes the effective default.
func (c *Config) DefaultOrchardURL() string {
	if c == nil {
		return ""
	}
	if host := normalizeOrchardHostKey(c.OrchardURL); host != "" {
		return host
	}
	if len(c.OrchardProfiles) == 1 {
		for host := range c.OrchardProfiles {
			return normalizeOrchardHostKey(host)
		}
	}
	return ""
}

// OrchardProfile returns the Orchard profile for the given base URL.
// Legacy top-level Orchard fields are treated as the profile for OrchardURL.
func (c *Config) OrchardProfile(baseURL string) OrchardProfile {
	if c == nil {
		return OrchardProfile{}
	}
	key := normalizeOrchardHostKey(baseURL)
	if key == "" {
		key = c.DefaultOrchardURL()
	}
	if key != "" {
		if profile, ok := c.OrchardProfiles[key]; ok {
			return profile
		}
	}

	legacyKey := normalizeOrchardHostKey(c.OrchardURL)
	if key == "" || (legacyKey != "" && key == legacyKey) {
		profile := OrchardProfile{
			Token:    c.Token,
			Username: c.Username,
			Owner:    c.Owner,
		}
		profile.normalize()
		return profile
	}
	return OrchardProfile{}
}

// SetOrchardProfile upserts the Orchard profile for a base URL.
func (c *Config) SetOrchardProfile(baseURL string, profile OrchardProfile) string {
	if c == nil {
		return ""
	}
	key := normalizeOrchardHostKey(baseURL)
	if key == "" {
		return ""
	}
	profile.normalize()
	if profile.isZero() {
		if c.OrchardProfiles != nil {
			delete(c.OrchardProfiles, key)
			if len(c.OrchardProfiles) == 0 {
				c.OrchardProfiles = nil
			}
		}
		return key
	}
	if c.OrchardProfiles == nil {
		c.OrchardProfiles = make(map[string]OrchardProfile)
	}
	c.OrchardProfiles[key] = profile
	return key
}

// OrchardProfileHosts returns all known Orchard profile host keys in sorted order.
func (c *Config) OrchardProfileHosts() []string {
	if c == nil || len(c.OrchardProfiles) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(c.OrchardProfiles))
	for host := range c.OrchardProfiles {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, configFileName), nil
}

func (c *Config) normalize() {
	if c == nil {
		return
	}
	if c.Version <= 0 {
		c.Version = currentConfigVersion
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Email = strings.TrimSpace(c.Email)
	c.OrchardURL = normalizeOrchardHostKey(c.OrchardURL)
	c.Token = strings.TrimSpace(c.Token)
	c.Username = strings.TrimSpace(c.Username)
	c.Owner = strings.TrimSpace(c.Owner)
	c.SigningKeyPath = strings.TrimSpace(c.SigningKeyPath)
	c.AIProvider = strings.TrimSpace(c.AIProvider)
	c.AIAPIKey = strings.TrimSpace(c.AIAPIKey)
	c.AIModel = strings.TrimSpace(c.AIModel)

	if len(c.OrchardProfiles) > 0 {
		normalized := make(map[string]OrchardProfile, len(c.OrchardProfiles))
		for host, profile := range c.OrchardProfiles {
			key := normalizeOrchardHostKey(host)
			if key == "" {
				continue
			}
			profile.normalize()
			if profile.isZero() {
				continue
			}
			normalized[key] = mergeOrchardProfiles(normalized[key], profile)
		}
		if len(normalized) > 0 {
			c.OrchardProfiles = normalized
		} else {
			c.OrchardProfiles = nil
		}
	}

	if c.OrchardURL != "" {
		legacy := OrchardProfile{
			Token:    c.Token,
			Username: c.Username,
			Owner:    c.Owner,
		}
		legacy.normalize()
		if !legacy.isZero() {
			c.SetOrchardProfile(c.OrchardURL, mergeOrchardProfiles(c.OrchardProfiles[c.OrchardURL], legacy))
		}
	}
}

func (p *OrchardProfile) normalize() {
	if p == nil {
		return
	}
	p.Token = strings.TrimSpace(p.Token)
	p.Username = strings.TrimSpace(p.Username)
	p.Owner = strings.TrimSpace(p.Owner)
}

func (p OrchardProfile) isZero() bool {
	return p.Token == "" && p.Username == "" && p.Owner == ""
}

func mergeOrchardProfiles(dst, src OrchardProfile) OrchardProfile {
	dst.normalize()
	src.normalize()
	if dst.Token == "" {
		dst.Token = src.Token
	}
	if dst.Username == "" {
		dst.Username = src.Username
	}
	if dst.Owner == "" {
		dst.Owner = src.Owner
	}
	return dst
}

func normalizeOrchardHostKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(strings.TrimSpace(u.Scheme))
	u.Host = strings.ToLower(strings.TrimSpace(u.Host))
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return strings.TrimRight(u.String(), "/")
}

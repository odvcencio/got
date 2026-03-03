// Package userconfig manages user-level graft configuration stored in
// ~/.config/graft/.
package userconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	currentConfigVersion = 1
	configFileName       = ".graftconfig"
)

// Config stores user-wide graft settings and credentials.
// Environment variables still take precedence over these values.
type Config struct {
	Version        int    `json:"version"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	OrchardURL     string `json:"orchard_url,omitempty"`
	Token          string `json:"token,omitempty"`
	Username       string `json:"username,omitempty"`
	Owner          string `json:"owner,omitempty"`
	SigningKeyPath string `json:"signing_key_path,omitempty"`
	AutoSign       bool   `json:"auto_sign,omitempty"`
	AIProvider     string `json:"ai_provider,omitempty"` // "claude" (default) or future providers
	AIAPIKey       string `json:"ai_api_key,omitempty"`  // API key for AI provider
	AIModel        string `json:"ai_model,omitempty"`    // model override (e.g. "claude-opus-4-20250514")
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
	c.OrchardURL = strings.TrimSpace(c.OrchardURL)
	c.Token = strings.TrimSpace(c.Token)
	c.Username = strings.TrimSpace(c.Username)
	c.Owner = strings.TrimSpace(c.Owner)
	c.SigningKeyPath = strings.TrimSpace(c.SigningKeyPath)
	c.AIProvider = strings.TrimSpace(c.AIProvider)
	c.AIAPIKey = strings.TrimSpace(c.AIAPIKey)
	c.AIModel = strings.TrimSpace(c.AIModel)
}

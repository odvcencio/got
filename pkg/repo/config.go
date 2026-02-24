package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config stores repository-local settings such as named remotes.
type Config struct {
	Remotes map[string]string `json:"remotes,omitempty"`
}

func (r *Repo) configPath() string {
	return filepath.Join(r.GotDir, "config.json")
}

// ReadConfig reads .got/config.json. Missing config returns an empty config.
func (r *Repo) ReadConfig() (*Config, error) {
	data, err := os.ReadFile(r.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Remotes: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("read config: unmarshal: %w", err)
	}
	if cfg.Remotes == nil {
		cfg.Remotes = make(map[string]string)
	}
	return &cfg, nil
}

// WriteConfig atomically writes .got/config.json.
func (r *Repo) WriteConfig(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Remotes == nil {
		cfg.Remotes = make(map[string]string)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("write config: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(r.GotDir, ".config-tmp-*")
	if err != nil {
		return fmt.Errorf("write config: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write config: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write config: close: %w", err)
	}
	if err := os.Rename(tmpName, r.configPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write config: rename: %w", err)
	}
	return nil
}

// SetRemote stores/updates a named remote URL in repository config.
func (r *Repo) SetRemote(name, remoteURL string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("set remote: remote name is required")
	}
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return fmt.Errorf("set remote: remote URL is required")
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		return err
	}
	cfg.Remotes[name] = remoteURL
	return r.WriteConfig(cfg)
}

// RemoteURL returns the configured URL for the given remote name.
func (r *Repo) RemoteURL(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("remote name is required")
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		return "", err
	}
	url, ok := cfg.Remotes[name]
	if !ok || strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("remote %q is not configured", name)
	}
	return url, nil
}

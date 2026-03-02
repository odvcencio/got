package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	var global bool
	var list bool

	cmd := &cobra.Command{
		Use:   "config [key] [value]",
		Short: "Get or set configuration options",
		Long: `Get or set graft configuration options.

Without --global, values are stored in the repository config (.graft/config.json).
With --global, values are stored in the user config (~/.graftconfig).

Supported keys: user.name, user.email

Examples:
  graft config user.name "Alice"
  graft config user.email "alice@example.com"
  graft config --global user.name "Alice"
  graft config user.name
  graft config --list`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return configList(cmd, global)
			}
			if len(args) == 0 {
				return fmt.Errorf("key is required (or use --list)")
			}
			key := args[0]
			if len(args) == 2 {
				return configSet(cmd, key, args[1], global)
			}
			return configGet(cmd, key, global)
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "use user-level config (~/.graftconfig)")
	cmd.Flags().BoolVar(&list, "list", false, "list all configuration values")

	return cmd
}

// configSet sets a config key to a value.
func configSet(cmd *cobra.Command, key, value string, global bool) error {
	if global {
		return configSetGlobal(key, value)
	}
	return configSetRepo(key, value)
}

func configSetGlobal(key, value string) error {
	cfg, err := userconfig.Load()
	if err != nil {
		return err
	}
	if err := applyUserConfigKey(cfg, key, value); err != nil {
		return err
	}
	return userconfig.Save(cfg)
}

func configSetRepo(key, value string) error {
	r, err := repo.Open(".")
	if err != nil {
		return err
	}
	cfg, err := r.ReadConfig()
	if err != nil {
		return err
	}
	if err := applyRepoConfigKey(cfg, key, value); err != nil {
		return err
	}
	return r.WriteConfig(cfg)
}

// applyUserConfigKey sets a known key on the user config.
func applyUserConfigKey(cfg *userconfig.Config, key, value string) error {
	switch key {
	case "user.name":
		cfg.Name = value
	case "user.email":
		cfg.Email = value
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

// applyRepoConfigKey sets a known key on the repo config.
func applyRepoConfigKey(cfg *repo.Config, key, value string) error {
	switch key {
	case "user.name":
		if cfg.User == nil {
			cfg.User = &repo.UserConfig{}
		}
		cfg.User.Name = value
	case "user.email":
		if cfg.User == nil {
			cfg.User = &repo.UserConfig{}
		}
		cfg.User.Email = value
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

// configGet retrieves and prints a config value.
func configGet(cmd *cobra.Command, key string, global bool) error {
	if global {
		return configGetGlobal(cmd, key)
	}
	return configGetWithFallback(cmd, key)
}

func configGetGlobal(cmd *cobra.Command, key string) error {
	cfg, err := userconfig.Load()
	if err != nil {
		return err
	}
	val, err := readUserConfigKey(cfg, key)
	if err != nil {
		return err
	}
	if val != "" {
		fmt.Fprintln(cmd.OutOrStdout(), val)
	}
	return nil
}

func configGetWithFallback(cmd *cobra.Command, key string) error {
	r, err := repo.Open(".")
	if err != nil {
		return err
	}
	cfg, err := r.ReadConfig()
	if err != nil {
		return err
	}
	val, err := readRepoConfigKey(cfg, key)
	if err != nil {
		return err
	}
	if val != "" {
		fmt.Fprintln(cmd.OutOrStdout(), val)
		return nil
	}
	// Fall back to global config.
	ucfg, err := userconfig.Load()
	if err != nil {
		return err
	}
	val, err = readUserConfigKey(ucfg, key)
	if err != nil {
		return err
	}
	if val != "" {
		fmt.Fprintln(cmd.OutOrStdout(), val)
	}
	return nil
}

// readUserConfigKey reads a known key from the user config.
func readUserConfigKey(cfg *userconfig.Config, key string) (string, error) {
	switch key {
	case "user.name":
		return cfg.Name, nil
	case "user.email":
		return cfg.Email, nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// readRepoConfigKey reads a known key from the repo config.
func readRepoConfigKey(cfg *repo.Config, key string) (string, error) {
	switch key {
	case "user.name":
		if cfg.User != nil {
			return cfg.User.Name, nil
		}
		return "", nil
	case "user.email":
		if cfg.User != nil {
			return cfg.User.Email, nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// configList prints all config values.
func configList(cmd *cobra.Command, global bool) error {
	var lines []string

	if global {
		cfg, err := userconfig.Load()
		if err != nil {
			return err
		}
		lines = formatUserConfig(cfg)
	} else {
		// Show repo config, then global config for completeness.
		r, err := repo.Open(".")
		if err != nil {
			return err
		}
		cfg, err := r.ReadConfig()
		if err != nil {
			return err
		}
		lines = formatRepoConfig(cfg)

		// Also show global config values.
		ucfg, err := userconfig.Load()
		if err == nil {
			for _, l := range formatUserConfig(ucfg) {
				lines = append(lines, l+" (global)")
			}
		}
	}

	for _, l := range lines {
		fmt.Fprintln(cmd.OutOrStdout(), l)
	}
	return nil
}

func formatUserConfig(cfg *userconfig.Config) []string {
	var lines []string
	if cfg.Name != "" {
		lines = append(lines, "user.name="+cfg.Name)
	}
	if cfg.Email != "" {
		lines = append(lines, "user.email="+cfg.Email)
	}
	if cfg.OrchardURL != "" {
		lines = append(lines, "orchard.url="+cfg.OrchardURL)
	}
	if cfg.Username != "" {
		lines = append(lines, "orchard.username="+cfg.Username)
	}
	if cfg.Owner != "" {
		lines = append(lines, "orchard.owner="+cfg.Owner)
	}
	if cfg.SigningKeyPath != "" {
		lines = append(lines, "signing.key="+cfg.SigningKeyPath)
	}
	if cfg.AutoSign {
		lines = append(lines, "signing.auto=true")
	}
	return lines
}

func formatRepoConfig(cfg *repo.Config) []string {
	var lines []string
	if cfg.User != nil {
		if cfg.User.Name != "" {
			lines = append(lines, "user.name="+cfg.User.Name)
		}
		if cfg.User.Email != "" {
			lines = append(lines, "user.email="+cfg.User.Email)
		}
	}
	for name, url := range cfg.Remotes {
		lines = append(lines, "remote."+name+".url="+url)
	}
	return lines
}


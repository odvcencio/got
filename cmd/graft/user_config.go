package main

import (
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/userconfig"
)

func loadUserConfig() *userconfig.Config {
	cfg, err := userconfig.Load()
	if err != nil || cfg == nil {
		return &userconfig.Config{}
	}
	return cfg
}

func configuredOrchardHost(explicit string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GRAFT_ORCHARD_URL")); v != "" {
		return v
	}
	cfg := loadUserConfig()
	return strings.TrimSpace(cfg.OrchardURL)
}

func configuredToken(explicit string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GRAFT_TOKEN")); v != "" {
		return v
	}
	cfg := loadUserConfig()
	return strings.TrimSpace(cfg.Token)
}

func configuredOwner() string {
	if v := strings.TrimSpace(os.Getenv("GRAFT_OWNER")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ORCHARD_OWNER")); v != "" {
		return v
	}
	cfg := loadUserConfig()
	if v := strings.TrimSpace(cfg.Owner); v != "" {
		return v
	}
	return strings.TrimSpace(cfg.Username)
}

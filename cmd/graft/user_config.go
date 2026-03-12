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
	return strings.TrimSpace(cfg.DefaultOrchardURL())
}

func configuredToken(explicit string) string {
	return configuredTokenForHost("", explicit)
}

func configuredTokenForHost(host, explicit string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GRAFT_TOKEN")); v != "" {
		return v
	}
	return strings.TrimSpace(configuredOrchardProfile(host).Token)
}

func configuredUsernameForHost(host string) string {
	return strings.TrimSpace(configuredOrchardProfile(host).Username)
}

func configuredOwner() string {
	return configuredOwnerForHost("")
}

func configuredOwnerForHost(host string) string {
	if v := strings.TrimSpace(os.Getenv("GRAFT_OWNER")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ORCHARD_OWNER")); v != "" {
		return v
	}
	profile := configuredOrchardProfile(host)
	if v := strings.TrimSpace(profile.Owner); v != "" {
		return v
	}
	return strings.TrimSpace(profile.Username)
}

func configuredOrchardProfile(host string) userconfig.OrchardProfile {
	cfg := loadUserConfig()
	return cfg.OrchardProfile(host)
}

package main

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/odvcencio/got/pkg/remote"
)

type remoteTransportKind string

const (
	remoteTransportGot remoteTransportKind = "got"
	remoteTransportGit remoteTransportKind = "git"
)

func parseRemoteSpec(raw string) (remoteTransportKind, string, error) {
	canonical, err := canonicalizeRemoteSpec(raw)
	if err != nil {
		return "", "", err
	}
	if shouldUseGitTransport(canonical) {
		return remoteTransportGit, canonical, nil
	}
	if _, err := remote.ParseEndpoint(canonical); err == nil {
		return remoteTransportGot, canonical, nil
	}
	if looksLikeGitRemote(canonical) {
		return remoteTransportGit, canonical, nil
	}
	return "", "", fmt.Errorf("unsupported remote %q", raw)
}

func shouldUseGitTransport(raw string) bool {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "git@") {
		return true
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return false
	}
	if strings.EqualFold(u.Scheme, "file") {
		return strings.TrimSpace(u.Path) != ""
	}
	if strings.TrimSpace(u.Host) == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if isKnownGitForgeHost(host) {
		return true
	}
	base := strings.ToLower(path.Base(strings.TrimSpace(u.Path)))
	return strings.HasSuffix(base, ".git")
}

func isKnownGitForgeHost(host string) bool {
	switch host {
	case "github.com", "gitlab.com", "bitbucket.org":
		return true
	default:
		return false
	}
}

func looksLikeGitRemote(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "git@") {
		return true
	}
	if strings.HasPrefix(s, "ssh://") {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		return false
	}
	if strings.EqualFold(u.Scheme, "file") {
		return strings.TrimSpace(u.Path) != ""
	}
	if strings.TrimSpace(u.Host) == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ssh", "git", "file":
		return strings.TrimSpace(u.Path) != ""
	default:
		return false
	}
}

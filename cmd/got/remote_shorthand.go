package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
)

const (
	defaultGothubBaseURL    = "https://gothub.dev"
	defaultGitHubBaseURL    = "https://github.com"
	defaultGitLabBaseURL    = "https://gitlab.com"
	defaultBitbucketBaseURL = "https://bitbucket.org"
)

// canonicalizeRemoteSpec expands shorthand forms like "gothub:owner/repo" into
// canonical Got protocol endpoints and normalizes host-only variations.
func canonicalizeRemoteSpec(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("remote URL is required")
	}
	if strings.Contains(raw, "://") {
		return raw, nil
	}

	provider, repoPath, ok := strings.Cut(raw, ":")
	if !ok {
		return raw, nil
	}
	provider = strings.TrimSpace(provider)
	repoPath = strings.TrimSpace(repoPath)
	if provider == "" || repoPath == "" {
		return "", fmt.Errorf("invalid remote shorthand %q", raw)
	}

	providerLower := strings.ToLower(provider)
	switch providerLower {
	case "gothub":
		owner, repoName, err := parseOwnerRepo(repoPath)
		if err != nil {
			return "", err
		}
		baseURL, err := normalizeBaseURL(os.Getenv("GOT_GOTHUB_URL"), defaultGothubBaseURL)
		if err != nil {
			return "", err
		}
		return joinGotEndpoint(baseURL, owner, repoName), nil
	case "github", "gh":
		owner, repoName, err := parseOwnerRepo(repoPath)
		if err != nil {
			return "", err
		}
		baseURL, err := normalizeBaseURL(os.Getenv("GOT_GITHUB_URL"), defaultGitHubBaseURL)
		if err != nil {
			return "", err
		}
		return joinGitEndpoint(baseURL, owner+"/"+repoName), nil
	case "gitlab", "gl":
		baseURL, err := normalizeBaseURL(os.Getenv("GOT_GITLAB_URL"), defaultGitLabBaseURL)
		if err != nil {
			return "", err
		}
		repoPath, err := parseGitRepoPath(repoPath)
		if err != nil {
			return "", err
		}
		return joinGitEndpoint(baseURL, repoPath), nil
	case "bitbucket", "bb":
		owner, repoName, err := parseOwnerRepo(repoPath)
		if err != nil {
			return "", err
		}
		baseURL, err := normalizeBaseURL(os.Getenv("GOT_BITBUCKET_URL"), defaultBitbucketBaseURL)
		if err != nil {
			return "", err
		}
		return joinGitEndpoint(baseURL, owner+"/"+repoName), nil
	default:
		// Allow host shorthand for self-hosted instances:
		//   code.example.com:owner/repo -> https://code.example.com/got/owner/repo
		if strings.Contains(provider, ".") || strings.EqualFold(provider, "localhost") {
			owner, repoName, err := parseOwnerRepo(repoPath)
			if err != nil {
				return "", err
			}
			baseURL, err := normalizeBaseURL("https://"+provider, defaultGothubBaseURL)
			if err != nil {
				return "", err
			}
			return joinGotEndpoint(baseURL, owner, repoName), nil
		}
	}

	return raw, nil
}

func normalizeBaseURL(raw, fallback string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = fallback
	}
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", candidate, err)
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("base URL must include scheme and host")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func parseOwnerRepo(raw string) (string, string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repository path must be owner/repo")
	}
	owner := strings.TrimSpace(parts[0])
	repoName := strings.TrimSpace(parts[1])
	if owner == "" || repoName == "" {
		return "", "", fmt.Errorf("repository path must include non-empty owner and repo")
	}
	return owner, repoName, nil
}

func parseGitRepoPath(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", fmt.Errorf("repository path must be owner/repo")
	}
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("repository path must be owner/repo")
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", fmt.Errorf("repository path must include non-empty segments")
		}
	}
	return strings.Join(parts, "/"), nil
}

func joinGotEndpoint(baseURL, owner, repo string) string {
	return strings.TrimRight(baseURL, "/") + path.Join("/got", owner, repo)
}

func joinGitEndpoint(baseURL, repoPath string) string {
	base := strings.TrimRight(baseURL, "/")
	repoPath = strings.Trim(strings.TrimSpace(repoPath), "/")
	if repoPath == "" {
		return base
	}
	full := base + "/" + repoPath
	if strings.HasSuffix(full, ".git") {
		return full
	}
	return full + ".git"
}

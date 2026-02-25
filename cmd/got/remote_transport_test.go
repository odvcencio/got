package main

import "testing"

func TestParseRemoteSpecGitHubShorthand(t *testing.T) {
	kind, url, err := parseRemoteSpec("github:alice/repo")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	if kind != remoteTransportGit {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGit)
	}
	if url != "https://github.com/alice/repo.git" {
		t.Fatalf("url = %q, want %q", url, "https://github.com/alice/repo.git")
	}
}

func TestParseRemoteSpecGitLabSubgroupShorthand(t *testing.T) {
	kind, url, err := parseRemoteSpec("gitlab:group/subgroup/repo")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	if kind != remoteTransportGit {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGit)
	}
	if url != "https://gitlab.com/group/subgroup/repo.git" {
		t.Fatalf("url = %q, want %q", url, "https://gitlab.com/group/subgroup/repo.git")
	}
}

func TestInferRepoNameFromRemote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://github.com/alice/repo.git", want: "repo"},
		{in: "https://gitlab.com/group/subgroup/repo.git", want: "repo"},
		{in: "git@github.com:alice/repo.git", want: "repo"},
		{in: "https://example.com/alice/repo", want: "repo"},
	}
	for _, tc := range tests {
		if got := inferRepoNameFromRemote(tc.in); got != tc.want {
			t.Fatalf("inferRepoNameFromRemote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseRemoteSpecFileURLIsGit(t *testing.T) {
	kind, _, err := parseRemoteSpec("file:///tmp/example/repo.git")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	if kind != remoteTransportGit {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGit)
	}
}

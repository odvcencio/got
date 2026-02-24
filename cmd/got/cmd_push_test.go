package main

import (
	"testing"

	"github.com/odvcencio/got/pkg/repo"
)

func TestResolvePushRefNames(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	tests := []struct {
		name       string
		branchArg  string
		wantLabel  string
		wantLocal  string
		wantRemote string
		wantErr    bool
	}{
		{
			name:       "short branch name",
			branchArg:  "main",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
		{
			name:       "full branch ref",
			branchArg:  "refs/heads/feature",
			wantLabel:  "branch feature",
			wantLocal:  "refs/heads/feature",
			wantRemote: "heads/feature",
		},
		{
			name:       "full tag ref",
			branchArg:  "refs/tags/v1.0.0",
			wantLabel:  "tag v1.0.0",
			wantLocal:  "refs/tags/v1.0.0",
			wantRemote: "tags/v1.0.0",
		},
		{
			name:      "unsupported ref namespace",
			branchArg: "refs/notes/release",
			wantErr:   true,
		},
		{
			name:       "infer from HEAD when empty",
			branchArg:  "",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, localRef, remoteRef, err := resolvePushRefNames(r, tc.branchArg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePushRefNames: %v", err)
			}
			if label != tc.wantLabel {
				t.Fatalf("label = %q, want %q", label, tc.wantLabel)
			}
			if localRef != tc.wantLocal {
				t.Fatalf("localRef = %q, want %q", localRef, tc.wantLocal)
			}
			if remoteRef != tc.wantRemote {
				t.Fatalf("remoteRef = %q, want %q", remoteRef, tc.wantRemote)
			}
		})
	}
}

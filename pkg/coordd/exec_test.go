package coordd

import (
	"strings"
	"testing"
)

func TestSelectExecBackend_HostDirectDegradesProfile(t *testing.T) {
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true})
	cfg := &GuardConfig{PreferredBackend: "host-direct"}

	backend, effective, degradations, err := selectExecBackend(nil, cfg, requested)
	if err != nil {
		t.Fatalf("selectExecBackend: %v", err)
	}
	if backend != "host-direct" {
		t.Fatalf("backend = %q, want host-direct", backend)
	}
	if effective.Name != "host_direct" {
		t.Fatalf("effective.Name = %q, want host_direct", effective.Name)
	}
	if effective.Network != NetworkAmbient {
		t.Fatalf("effective.Network = %q, want %q", effective.Network, NetworkAmbient)
	}
	if len(degradations) == 0 {
		t.Fatal("expected degradations for host-direct backend")
	}
}

func TestBuildContainerInvocation_PodmanRepoWrite(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Selector: "shell:touch note.txt",
			Argv:     []string{"touch", "note.txt"},
		},
	}
	requested := ResolveRuntimeProfile("repo_write", ActionPolicyAction{WritesFilesystem: true})

	invocation, err := BuildContainerInvocation("podman", "docker.io/library/alpine:3.20", "/tmp/repo", "/workspace/subdir", input, "Allow", requested, requested)
	if err != nil {
		t.Fatalf("BuildContainerInvocation: %v", err)
	}
	if invocation.Runtime != "podman" {
		t.Fatalf("Runtime = %q, want podman", invocation.Runtime)
	}
	joined := strings.Join(invocation.Args, " ")
	if !strings.Contains(joined, "--network none") {
		t.Fatalf("expected network none in %q", joined)
	}
	if !strings.Contains(joined, "-v /tmp/repo:/workspace:rw") {
		t.Fatalf("expected rw workspace mount in %q", joined)
	}
	if !strings.Contains(joined, "--workdir /workspace/subdir") {
		t.Fatalf("expected workdir in %q", joined)
	}
}

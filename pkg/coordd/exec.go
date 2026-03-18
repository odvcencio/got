package coordd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
)

type ExecResult struct {
	Decision         *ActionPolicyDecision `json:"decision,omitempty"`
	ExitCode         int                   `json:"exit_code"`
	Backend          string                `json:"backend,omitempty"`
	RequestedProfile RuntimeProfile        `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile        `json:"effective_profile,omitempty"`
	Degradations     []string              `json:"degradations,omitempty"`
	SnapshotID       string                `json:"snapshot_id,omitempty"`
}

type ExecIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("command exited with status %d", e.Code)
}

func (e *ExitCodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ExitCodeError) ExitCode() int {
	if e == nil || e.Code == 0 {
		return 1
	}
	return e.Code
}

func ExecuteGuarded(r *repo.Repo, input ActionPolicyInput, decision *ActionPolicyDecision) (*ExecResult, error) {
	return ExecuteGuardedWithIO(r, input, decision, ExecIO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

func ExecuteGuardedWithIO(r *repo.Repo, input ActionPolicyInput, decision *ActionPolicyDecision, execIO ExecIO) (*ExecResult, error) {
	if len(input.Action.Argv) == 0 {
		return nil, fmt.Errorf("missing command")
	}
	if decision == nil {
		return nil, fmt.Errorf("missing policy decision")
	}

	requestedProfile := ResolveRuntimeProfile(decision.Profile, input.Action)
	result := &ExecResult{
		Decision:         decision,
		RequestedProfile: requestedProfile,
		EffectiveProfile: requestedProfile,
	}
	if decision.Action == "HardBlock" {
		result.ExitCode = 126
		return result, &ExitCodeError{
			Code: 126,
			Err:  fmt.Errorf("coordd blocked action: %s", decision.Reason),
		}
	}

	cfg, err := loadGuardConfigForExec(r)
	if err != nil {
		return nil, err
	}

	if requestedProfile.RequireSnapshot && r != nil {
		snapshotID, snapshotErr := captureExecSnapshot(r, input.Session.AgentID)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		result.SnapshotID = snapshotID
	}

	backendName, effectiveProfile, degradations, err := selectExecBackend(r, cfg, requestedProfile)
	if err != nil {
		return nil, err
	}
	result.Backend = backendName
	result.EffectiveProfile = effectiveProfile
	result.Degradations = append(result.Degradations, degradations...)

	started := time.Now().UTC()
	if r != nil {
		_ = AppendEvent(r.GraftDir, Event{
			ID:        newID(),
			Type:      "action_exec_started",
			Timestamp: started,
			RepoRoot:  input.Repo.Root,
			AgentID:   input.Session.AgentID,
			Data: map[string]any{
				"selector":          input.Action.Selector,
				"program":           input.Action.Program,
				"decision":          decision.Action,
				"backend":           backendName,
				"requested_profile": requestedProfile,
				"effective_profile": effectiveProfile,
				"degradations":      degradations,
				"snapshot_id":       result.SnapshotID,
			},
		})
	}

	runErr := runWithBackend(backendName, r, input, decision.Action, requestedProfile, effectiveProfile, execIO)
	exitCode := 0
	status := "ok"
	if runErr != nil {
		status = "error"
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if coded, ok := runErr.(*ExitCodeError); ok {
			exitCode = coded.ExitCode()
		} else {
			exitCode = 127
		}
	}
	result.ExitCode = exitCode

	if r != nil {
		_ = AppendEvent(r.GraftDir, Event{
			ID:        newID(),
			Type:      "action_exec_finished",
			Timestamp: time.Now().UTC(),
			RepoRoot:  input.Repo.Root,
			AgentID:   input.Session.AgentID,
			Data: map[string]any{
				"selector":          input.Action.Selector,
				"program":           input.Action.Program,
				"decision":          decision.Action,
				"backend":           backendName,
				"requested_profile": requestedProfile,
				"effective_profile": effectiveProfile,
				"degradations":      degradations,
				"duration_ms":       time.Since(started).Milliseconds(),
				"status":            status,
				"exit_code":         exitCode,
				"snapshot_id":       result.SnapshotID,
			},
		})
	}

	if runErr != nil {
		return result, &ExitCodeError{Code: exitCode, Err: runErr}
	}
	return result, nil
}

func loadGuardConfigForExec(r *repo.Repo) (*GuardConfig, error) {
	if r == nil {
		return &GuardConfig{
			Mode:             "advisory",
			PreferredBackend: "auto",
			ContainerRuntime: "auto",
		}, nil
	}
	return LoadGuardConfig(r.GraftDir)
}

func captureExecSnapshot(r *repo.Repo, activeAgentID string) (string, error) {
	statusEntries, err := r.Status()
	if err != nil {
		return "", fmt.Errorf("status for snapshot: %w", err)
	}
	snapshot, err := CaptureSnapshot(r, activeAgentID, statusEntries, 256)
	if err != nil {
		return "", fmt.Errorf("capture snapshot: %w", err)
	}
	if snapshot == nil {
		return "", nil
	}
	return snapshot.ID, nil
}

func selectExecBackend(r *repo.Repo, cfg *GuardConfig, requested RuntimeProfile) (string, RuntimeProfile, []string, error) {
	preference := "auto"
	if cfg != nil && cfg.PreferredBackend != "" {
		preference = cfg.PreferredBackend
	}

	switch preference {
	case "auto":
		if canUseContainer(r, cfg, requested) {
			return "container", requested, nil, nil
		}
		if canUseBwrap(r, requested) {
			return "host-bwrap", requested, nil, nil
		}
		effective, degradations := directEffectiveProfile(requested)
		return "host-direct", effective, degradations, nil
	case "host-direct":
		effective, degradations := directEffectiveProfile(requested)
		return "host-direct", effective, degradations, nil
	case "host-bwrap":
		if !canUseBwrap(r, requested) {
			return "", RuntimeProfile{}, nil, fmt.Errorf("coordd host-bwrap backend unavailable for requested profile %s", requested.Name)
		}
		return "host-bwrap", requested, nil, nil
	case "container":
		if !canUseContainer(r, cfg, requested) {
			return "", RuntimeProfile{}, nil, fmt.Errorf("coordd container backend unavailable for requested profile %s", requested.Name)
		}
		return "container", requested, nil, nil
	default:
		return "", RuntimeProfile{}, nil, fmt.Errorf("unknown coordd backend preference %q", preference)
	}
}

func directEffectiveProfile(requested RuntimeProfile) (RuntimeProfile, []string) {
	effective := requested
	effective.Name = "host_direct"
	effective.FilesystemScope = FilesystemScopeHostProc
	effective.Network = NetworkAmbient
	effective.DeleteScope = DeleteScopeAmbient

	var degradations []string
	if requested.FilesystemScope != "" && requested.FilesystemScope != FilesystemScopeHostProc {
		degradations = append(degradations, "filesystem scope is not enforced in host-direct backend")
	}
	if requested.Network == NetworkDeny {
		degradations = append(degradations, "network deny is not enforced in host-direct backend")
	}
	if requested.DeleteScope != "" && requested.DeleteScope != DeleteScopeAmbient {
		degradations = append(degradations, "delete scope is not enforced in host-direct backend")
	}
	return effective, degradations
}

func canUseBwrap(r *repo.Repo, requested RuntimeProfile) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return false
	}
	if requested.FilesystemScope == FilesystemScopeRepoRO || requested.FilesystemScope == FilesystemScopeRepoRW {
		return r != nil && r.RootDir != ""
	}
	return true
}

func canUseContainer(r *repo.Repo, cfg *GuardConfig, requested RuntimeProfile) bool {
	if _, err := resolveContainerRuntime(cfg); err != nil {
		return false
	}
	if cfg == nil || strings.TrimSpace(cfg.ContainerImage) == "" {
		return false
	}
	if requested.FilesystemScope == FilesystemScopeRepoRO || requested.FilesystemScope == FilesystemScopeRepoRW {
		return r != nil && r.RootDir != ""
	}
	return true
}

func runWithBackend(backend string, r *repo.Repo, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, execIO ExecIO) error {
	switch backend {
	case "host-direct":
		return runDirect(input, decisionAction, requested, effective, execIO)
	case "host-bwrap":
		return runBwrap(r, input, decisionAction, requested, effective, execIO)
	case "container":
		cfg, err := loadGuardConfigForExec(r)
		if err != nil {
			return err
		}
		return runContainer(r, cfg, input, decisionAction, requested, effective, execIO)
	default:
		return fmt.Errorf("unknown coordd backend %q", backend)
	}
}

func runDirect(input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, execIO ExecIO) error {
	cmd := exec.Command(input.Action.Argv[0], input.Action.Argv[1:]...)
	cmd.Stdin = execInput(execIO.Stdin, os.Stdin)
	cmd.Stdout = execOutput(execIO.Stdout, os.Stdout)
	cmd.Stderr = execOutput(execIO.Stderr, os.Stderr)
	cmd.Env = append(os.Environ(),
		"GRAFT_COORDD_GUARDED=1",
		"GRAFT_COORDD_SELECTOR="+input.Action.Selector,
		"GRAFT_COORDD_POLICY_ACTION="+decisionAction,
		"GRAFT_COORDD_REQUESTED_PROFILE="+requested.Name,
		"GRAFT_COORDD_EFFECTIVE_PROFILE="+effective.Name,
	)
	return cmd.Run()
}

func runBwrap(r *repo.Repo, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, execIO ExecIO) error {
	if !canUseBwrap(r, requested) {
		return fmt.Errorf("host-bwrap backend unavailable")
	}

	cwd, err := os.Getwd()
	if err != nil && r != nil {
		cwd = r.RootDir
	}
	if cwd == "" && r != nil {
		cwd = r.RootDir
	}

	args := []string{
		"--die-with-parent",
		"--new-session",
		"--proc", "/proc",
		"--dev", "/dev",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
	}
	if requested.Network == NetworkDeny {
		args = append(args, "--unshare-net")
	}
	if r != nil {
		switch requested.FilesystemScope {
		case FilesystemScopeRepoRW:
			args = append(args, "--bind", r.RootDir, r.RootDir)
		case FilesystemScopeRepoRO:
			args = append(args, "--ro-bind", r.RootDir, r.RootDir)
		}
	}
	if cwd != "" {
		if r != nil {
			if rel, relErr := filepath.Rel(r.RootDir, cwd); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				args = append(args, "--chdir", cwd)
			} else {
				args = append(args, "--chdir", r.RootDir)
			}
		} else {
			args = append(args, "--chdir", cwd)
		}
	}
	args = append(args, "--")
	args = append(args, input.Action.Argv...)

	cmd := exec.Command("bwrap", args...)
	cmd.Stdin = execInput(execIO.Stdin, os.Stdin)
	cmd.Stdout = execOutput(execIO.Stdout, os.Stdout)
	cmd.Stderr = execOutput(execIO.Stderr, os.Stderr)
	cmd.Env = append(os.Environ(),
		"GRAFT_COORDD_GUARDED=1",
		"GRAFT_COORDD_SELECTOR="+input.Action.Selector,
		"GRAFT_COORDD_POLICY_ACTION="+decisionAction,
		"GRAFT_COORDD_REQUESTED_PROFILE="+requested.Name,
		"GRAFT_COORDD_EFFECTIVE_PROFILE="+effective.Name,
		"TMPDIR=/tmp",
	)
	return cmd.Run()
}

type ContainerInvocation struct {
	Runtime string
	Args    []string
}

func resolveContainerRuntime(cfg *GuardConfig) (string, error) {
	preference := "auto"
	if cfg != nil && strings.TrimSpace(cfg.ContainerRuntime) != "" {
		preference = strings.TrimSpace(cfg.ContainerRuntime)
	}
	switch preference {
	case "auto":
		if _, err := exec.LookPath("podman"); err == nil {
			return "podman", nil
		}
		if _, err := exec.LookPath("docker"); err == nil {
			return "docker", nil
		}
		return "", fmt.Errorf("no supported container runtime found (podman/docker)")
	case "podman", "docker":
		if _, err := exec.LookPath(preference); err != nil {
			return "", fmt.Errorf("container runtime %s not found", preference)
		}
		return preference, nil
	default:
		return "", fmt.Errorf("unsupported container runtime %q", preference)
	}
}

func containerWorkspacePaths(r *repo.Repo) (string, string, error) {
	hostRoot := ""
	if r != nil && r.RootDir != "" {
		hostRoot = r.RootDir
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		hostRoot = cwd
	}

	containerRoot := "/workspace"
	cwd, err := os.Getwd()
	if err != nil {
		return hostRoot, containerRoot, nil
	}
	rel, relErr := filepath.Rel(hostRoot, cwd)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return hostRoot, containerRoot, nil
	}
	if rel == "." {
		return hostRoot, containerRoot, nil
	}
	return hostRoot, filepath.Join(containerRoot, filepath.ToSlash(rel)), nil
}

func BuildContainerInvocation(runtimeName, image, hostRoot, containerWorkdir string, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile) (*ContainerInvocation, error) {
	runtimeName = strings.TrimSpace(runtimeName)
	image = strings.TrimSpace(image)
	if runtimeName == "" {
		return nil, fmt.Errorf("missing container runtime")
	}
	if image == "" {
		return nil, fmt.Errorf("missing container image")
	}
	if hostRoot == "" {
		return nil, fmt.Errorf("missing host root for container mount")
	}
	if containerWorkdir == "" {
		containerWorkdir = "/workspace"
	}

	args := []string{"run", "--rm", "--init", "--read-only"}
	switch runtimeName {
	case "podman":
		args = append(args, "--userns=keep-id", "--security-opt", "no-new-privileges")
	case "docker":
		args = append(args, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()), "--security-opt", "no-new-privileges")
	default:
		return nil, fmt.Errorf("unsupported container runtime %q", runtimeName)
	}

	args = append(args,
		"--cap-drop=ALL",
		"--pids-limit=256",
		"--tmpfs", "/tmp:rw,nosuid,nodev",
		"--tmpfs", "/home/coordd:rw,nosuid,nodev",
		"--workdir", containerWorkdir,
		"--env", "HOME=/home/coordd",
		"--env", "GRAFT_COORDD_GUARDED=1",
		"--env", "GRAFT_COORDD_SELECTOR="+input.Action.Selector,
		"--env", "GRAFT_COORDD_POLICY_ACTION="+decisionAction,
		"--env", "GRAFT_COORDD_REQUESTED_PROFILE="+requested.Name,
		"--env", "GRAFT_COORDD_EFFECTIVE_PROFILE="+effective.Name,
	)

	if requested.Network == NetworkDeny {
		args = append(args, "--network", "none")
	}

	mountMode := ":ro"
	if requested.FilesystemScope == FilesystemScopeRepoRW {
		mountMode = ":rw"
	}
	args = append(args, "-v", hostRoot+":/workspace"+mountMode)
	args = append(args, image)
	args = append(args, input.Action.Argv...)
	return &ContainerInvocation{
		Runtime: runtimeName,
		Args:    args,
	}, nil
}

func runContainer(r *repo.Repo, cfg *GuardConfig, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, execIO ExecIO) error {
	runtimeName, err := resolveContainerRuntime(cfg)
	if err != nil {
		return err
	}
	hostRoot, containerWorkdir, err := containerWorkspacePaths(r)
	if err != nil {
		return fmt.Errorf("resolve container workspace: %w", err)
	}
	invocation, err := BuildContainerInvocation(runtimeName, cfg.ContainerImage, hostRoot, containerWorkdir, input, decisionAction, requested, effective)
	if err != nil {
		return err
	}

	cmd := exec.Command(invocation.Runtime, invocation.Args...)
	cmd.Stdin = execInput(execIO.Stdin, os.Stdin)
	cmd.Stdout = execOutput(execIO.Stdout, os.Stdout)
	cmd.Stderr = execOutput(execIO.Stderr, os.Stderr)
	return cmd.Run()
}

func execInput(current io.Reader, fallback *os.File) io.Reader {
	if current != nil {
		return current
	}
	return fallback
}

func execOutput(current io.Writer, fallback *os.File) io.Writer {
	if current != nil {
		return current
	}
	return fallback
}

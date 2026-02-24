package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGotCLIClonePushPullAgainstGothub(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("GOT_E2E_GOTHUB") != "1" {
		t.Skip("set GOT_E2E_GOTHUB=1 to run gothub e2e test")
	}

	gotRoot := projectRoot(t)
	gothubRoot := filepath.Clean(filepath.Join(gotRoot, "..", "gothub"))
	if _, err := os.Stat(filepath.Join(gothubRoot, "cmd", "gothub", "main.go")); err != nil {
		t.Skipf("gothub source not found at %s", gothubRoot)
	}

	gotBin := buildBinary(t, gotRoot, "got", "./cmd/got")
	gothubBin := buildBinary(t, gothubRoot, "gothub", "./cmd/gothub")

	baseURL, stop := startGothub(t, gothubBin)
	defer stop()

	token := registerUser(t, baseURL, "alice", "alice@example.com", "secret123")
	createRepo(t, baseURL, token, "demo")

	remoteURL := strings.Replace(baseURL, "http://", "http://alice:secret123@", 1) + "/got/alice/demo"

	repo1 := filepath.Join(t.TempDir(), "repo1")
	runCommand(t, "", gotBin, "init", repo1)
	writeText(t, filepath.Join(repo1, "main.go"), "package main\n\nfunc main() {}\n")
	runCommand(t, repo1, gotBin, "add", "main.go")
	runCommand(t, repo1, gotBin, "commit", "-m", "initial", "--author", "alice")
	runCommand(t, repo1, gotBin, "remote", "add", "origin", remoteURL)
	runCommand(t, repo1, gotBin, "push", "origin", "main")

	cloneDir := filepath.Join(t.TempDir(), "clone")
	runCommand(t, "", gotBin, "clone", remoteURL, cloneDir)

	got := readText(t, filepath.Join(cloneDir, "main.go"))
	if !strings.Contains(got, "func main() {}") {
		t.Fatalf("clone file content mismatch:\n%s", got)
	}

	writeText(t, filepath.Join(cloneDir, "main.go"), "package main\n\nfunc main() {}\nfunc feature() {}\n")
	runCommand(t, cloneDir, gotBin, "add", "main.go")
	runCommand(t, cloneDir, gotBin, "commit", "-m", "feature", "--author", "alice")
	runCommand(t, cloneDir, gotBin, "push", "origin", "main")

	runCommand(t, repo1, gotBin, "pull", "origin", "main")
	updated := readText(t, filepath.Join(repo1, "main.go"))
	if !strings.Contains(updated, "func feature() {}") {
		t.Fatalf("pull did not update working tree:\n%s", updated)
	}

	runCommand(t, cloneDir, gotBin, "tag", "-a", "-m", "release 1.0.0", "v1.0.0")
	runCommand(t, cloneDir, gotBin, "push", "origin", "refs/tags/v1.0.0")
	refs := listProtocolRefs(t, remoteURL)
	if strings.TrimSpace(refs["tags/v1.0.0"]) == "" {
		t.Fatalf("expected pushed tag refs/tags/v1.0.0 to exist on remote, refs=%v", refs)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func buildBinary(t *testing.T, root, name, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, string(out))
	}
	return bin
}

func runCommand(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "USER=alice")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed (%s %s): %v\n%s", bin, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func writeText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func startGothub(t *testing.T, gothubBin string) (string, func()) {
	t.Helper()
	work := t.TempDir()
	port := freePort(t)
	cfgPath := filepath.Join(work, "config.yml")
	cfg := fmt.Sprintf(`server:
  host: "127.0.0.1"
  port: %d
database:
  driver: "sqlite"
  dsn: "%s"
storage:
  path: "%s"
auth:
  jwt_secret: "0123456789abcdef0123456789abcdef"
  token_duration: "24h"
`, port, filepath.Join(work, "gothub.db"), filepath.Join(work, "repos"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, gothubBin, "serve", "-config", cfgPath)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start gothub: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForServer(baseURL + "/api/v1/user"); err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("gothub did not become ready: %v\nlogs:\n%s", err, logs.String())
	}

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}
	return baseURL, stop
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForServer(url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

func registerUser(t *testing.T, baseURL, username, email, password string) string {
	t.Helper()
	payload := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	raw, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register failed (%d): %s", resp.StatusCode, string(body))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if strings.TrimSpace(out.Token) == "" {
		t.Fatalf("register returned empty token")
	}
	return out.Token
}

func createRepo(t *testing.T, baseURL, token, name string) {
	t.Helper()
	payload := map[string]any{
		"name":        name,
		"description": "",
		"private":     false,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/repos", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("create repo request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create repo request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo failed (%d): %s", resp.StatusCode, string(body))
	}
}

func listProtocolRefs(t *testing.T, remoteURL string) map[string]string {
	t.Helper()
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		t.Fatalf("parse remote url %q: %v", remoteURL, err)
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(remoteURL, "/")+"/refs", nil)
	if err != nil {
		t.Fatalf("build refs request: %v", err)
	}
	if parsed.User != nil {
		user := parsed.User.Username()
		pass, _ := parsed.User.Password()
		if strings.TrimSpace(user) != "" {
			req.SetBasicAuth(user, pass)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list refs request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list refs failed (%d): %s", resp.StatusCode, string(body))
	}

	out := map[string]string{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode refs response: %v", err)
	}
	return out
}

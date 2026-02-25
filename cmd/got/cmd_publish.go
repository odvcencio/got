package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

type publishRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
}

type publishErrorResponse struct {
	Error string `json:"error"`
}

func newPublishCmd() *cobra.Command {
	var (
		host        string
		remoteName  string
		branch      string
		privateRepo bool
		noCreate    bool
		description string
	)

	cmd := &cobra.Command{
		Use:   "publish [owner/repo]",
		Short: "Create a remote repo on Gothub, set origin, and push current branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			owner, repoName, err := resolvePublishTarget(args, r.RootDir)
			if err != nil {
				return err
			}

			baseURL, err := normalizeBaseURL(host, defaultGothubBaseURL)
			if err != nil {
				return err
			}
			remoteURL := joinGotEndpoint(baseURL, owner, repoName)

			if !noCreate {
				if err := createRemoteRepository(cmd, baseURL, repoName, description, privateRepo); err != nil {
					return err
				}
			}

			if err := r.SetRemote(remoteName, remoteURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "configured %s -> %s\n", remoteName, remoteURL)

			pushBranchName := strings.TrimSpace(branch)
			if pushBranchName == "" {
				pushBranchName, err = r.CurrentBranch()
				if err != nil {
					return err
				}
				if strings.TrimSpace(pushBranchName) == "" {
					return fmt.Errorf("cannot infer branch while HEAD is detached; pass --branch")
				}
			}

			_, remoteURL, transport, err := resolveRemoteNameAndSpec(r, remoteName)
			if err != nil {
				return err
			}
			if transport != remoteTransportGot {
				return fmt.Errorf("publish currently supports gothub/got remotes only")
			}
			return pushBranchGot(cmd, r, remoteName, remoteURL, pushBranchName, false)
		},
	}

	cmd.Flags().StringVar(&host, "host", os.Getenv("GOT_GOTHUB_URL"), "Gothub base URL (default: GOT_GOTHUB_URL or https://gothub.dev)")
	cmd.Flags().StringVar(&remoteName, "remote", "origin", "remote name to configure")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch to publish (default: current branch)")
	cmd.Flags().BoolVar(&privateRepo, "private", false, "create repository as private")
	cmd.Flags().BoolVar(&noCreate, "no-create", false, "skip API repo creation and only configure remote + push")
	cmd.Flags().StringVar(&description, "description", "", "repository description used when creating the remote repo")
	return cmd
}

func resolvePublishTarget(args []string, rootDir string) (string, string, error) {
	target := ""
	if len(args) > 0 {
		target = strings.TrimSpace(args[0])
	}
	if target == "" {
		owner := strings.TrimSpace(os.Getenv("GOT_OWNER"))
		if owner == "" {
			owner = strings.TrimSpace(os.Getenv("GOTHUB_OWNER"))
		}
		if owner == "" {
			return "", "", fmt.Errorf("owner is required (pass owner/repo or set GOT_OWNER)")
		}
		repoName := filepath.Base(rootDir)
		if strings.TrimSpace(repoName) == "" || repoName == "." || repoName == string(filepath.Separator) {
			return "", "", fmt.Errorf("cannot infer repository name from current directory")
		}
		return owner, repoName, nil
	}
	return parseOwnerRepo(target)
}

func createRemoteRepository(cmd *cobra.Command, baseURL, name, description string, privateRepo bool) error {
	payload, err := json.Marshal(publishRepoRequest{
		Name:        name,
		Description: description,
		Private:     privateRepo,
	})
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/repos"
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	token := strings.TrimSpace(os.Getenv("GOT_TOKEN"))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("create remote repository: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		fmt.Fprintf(cmd.OutOrStdout(), "created remote repository %q\n", name)
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	errMessage := strings.TrimSpace(string(body))
	var parsed publishErrorResponse
	if json.Unmarshal(body, &parsed) == nil && strings.TrimSpace(parsed.Error) != "" {
		errMessage = strings.TrimSpace(parsed.Error)
	}
	lower := strings.ToLower(errMessage)
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(lower, "already") && strings.Contains(lower, "exist") {
		fmt.Fprintf(cmd.OutOrStdout(), "remote repository %q already exists\n", name)
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if token == "" {
			return fmt.Errorf("create remote repository failed (%d): set GOT_TOKEN or use --no-create", resp.StatusCode)
		}
		return fmt.Errorf("create remote repository failed (%d): %s", resp.StatusCode, errMessage)
	}
	return fmt.Errorf("create remote repository failed (%d): %s", resp.StatusCode, errMessage)
}

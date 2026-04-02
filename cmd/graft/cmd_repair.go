package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair or rebuild local graft metadata",
	}
	cmd.AddCommand(newRepairReseedCmd())
	return cmd
}

func newRepairReseedCmd() *cobra.Command {
	var yes bool
	var gitRef string

	cmd := &cobra.Command{
		Use:   "reseed",
		Short: "Replace local .graft state with a fresh snapshot from Git",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("repair reseed replaces the local .graft directory; re-run with --yes")
			}

			rootDir, err := gitTopLevel(cmd.Context(), ".")
			if err != nil {
				return err
			}

			existingGraftDir := filepath.Join(rootDir, ".graft")
			preservedConfig, err := readReseedConfig(existingGraftDir)
			if err != nil {
				return err
			}

			tempDir, err := os.MkdirTemp("", "graft-reseed-*")
			if err != nil {
				return fmt.Errorf("create reseed temp dir: %w", err)
			}
			defer os.RemoveAll(tempDir)

			if err := extractGitArchive(cmd.Context(), rootDir, gitRef, tempDir); err != nil {
				return err
			}

			ignoreFiles, err := stashRootIgnoreFiles(tempDir)
			if err != nil {
				return err
			}

			tmpRepo, err := repo.Init(tempDir)
			if err != nil {
				return err
			}
			if preservedConfig != nil {
				if err := tmpRepo.WriteConfig(preservedConfig); err != nil {
					return err
				}
			}

			if branch := gitBranchForReseed(cmd.Context(), rootDir, gitRef); branch != "" {
				if err := tmpRepo.SetHeadSymbolic("refs/heads/" + branch); err != nil {
					return err
				}
			}

			if hasNonGraftFiles(tempDir) {
				if err := tmpRepo.Add([]string{"."}); err != nil {
					return err
				}
			}

			if err := restoreRootIgnoreFiles(ignoreFiles); err != nil {
				return err
			}
			if len(ignoreFiles) > 0 {
				paths := make([]string, 0, len(ignoreFiles))
				for _, file := range ignoreFiles {
					paths = append(paths, file.RelPath)
				}
				if err := tmpRepo.Add(paths); err != nil {
					return err
				}
			}

			if gitbridge.DetectGitRepo(rootDir) {
				if err := ensureBridgeScaffold(tmpRepo); err != nil {
					return err
				}
				if err := writeBridgeHashMap(tmpRepo); err != nil {
					return err
				}
			}

			var commitHash string
			staging, err := tmpRepo.ReadStaging()
			if err != nil {
				return err
			}
			if len(staging.Entries) > 0 {
				author := gitCommitAuthor(cmd.Context(), rootDir, gitRef)
				if strings.TrimSpace(author) == "" {
					author = tmpRepo.ResolveAuthor()
				}
				msg := reseedCommitMessage(cmd.Context(), rootDir, gitRef)
				h, err := tmpRepo.Commit(msg, author)
				if err != nil {
					return err
				}
				commitHash = string(h)
			}

			backupPath := ""
			if _, err := os.Stat(existingGraftDir); err == nil {
				backupPath = rootDir + ".graft-backup-" + time.Now().Format("20060102-150405")
				if err := os.Rename(existingGraftDir, backupPath); err != nil {
					return fmt.Errorf("backup existing .graft: %w", err)
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat existing .graft: %w", err)
			}

			newGraftDir := filepath.Join(tempDir, ".graft")
			if err := os.Rename(newGraftDir, existingGraftDir); err != nil {
				if backupPath != "" {
					_ = os.Rename(backupPath, existingGraftDir)
				}
				return fmt.Errorf("install reseeded .graft: %w", err)
			}

			if gitbridge.DetectGitRepo(rootDir) {
				if err := ensureGraftExcludedFromGit(rootDir); err != nil {
					return err
				}
			}

			if commitHash != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "reseeded .graft from git %s at %s\n", gitRef, shortHashString(commitHash))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "reseeded empty .graft from git %s\n", gitRef)
			}
			if backupPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "backup: %s\n", backupPath)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "replace the current .graft directory without prompting")
	cmd.Flags().StringVar(&gitRef, "git-ref", "HEAD", "git ref to snapshot into the new graft store")
	return cmd
}

type stashedIgnoreFile struct {
	RelPath string
	Path    string
	Data    []byte
	Mode    os.FileMode
}

func gitTopLevel(ctx context.Context, path string) (string, error) {
	output, err := runGitCaptureWithLabel(ctx, path, "git-repair:toplevel", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve git toplevel: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func extractGitArchive(ctx context.Context, rootDir, gitRef, destDir string) error {
	tarFile, err := os.CreateTemp("", "graft-reseed-archive-*.tar")
	if err != nil {
		return fmt.Errorf("create git archive temp file: %w", err)
	}
	tarPath := tarFile.Name()
	defer os.Remove(tarPath)
	defer tarFile.Close()

	if err := runGitStreamingWithLabel(ctx, rootDir, tarFile, io.Discard, "git-repair:archive", "archive", "--format=tar", gitRef); err != nil {
		return err
	}
	if _, err := tarFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind git archive temp file: %w", err)
	}
	return extractTar(tarFile, destDir)
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read git archive: %w", err)
		}
		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(filepath.Separator)) && filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("archive entry %q escapes destination", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("extract dir %q: %w", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract dir for %q: %w", hdr.Name, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("extract file %q: %w", hdr.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("extract file %q: %w", hdr.Name, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close extracted file %q: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract symlink dir %q: %w", hdr.Name, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("extract symlink %q: %w", hdr.Name, err)
			}
		}
	}
}

func stashRootIgnoreFiles(rootDir string) ([]stashedIgnoreFile, error) {
	names := []string{".graftignore", ".gotignore", ".gitignore"}
	out := make([]stashedIgnoreFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(rootDir, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		out = append(out, stashedIgnoreFile{
			RelPath: name,
			Path:    path,
			Data:    data,
			Mode:    info.Mode().Perm(),
		})
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove %s during reseed: %w", name, err)
		}
	}
	return out, nil
}

func restoreRootIgnoreFiles(files []stashedIgnoreFile) error {
	for _, file := range files {
		if err := os.WriteFile(file.Path, file.Data, file.Mode); err != nil {
			return fmt.Errorf("restore %s: %w", file.RelPath, err)
		}
	}
	return nil
}

func hasNonGraftFiles(rootDir string) bool {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == ".graft" {
			continue
		}
		return true
	}
	return false
}

func readReseedConfig(existingGraftDir string) (*repo.Config, error) {
	cfgPath := filepath.Join(existingGraftDir, "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat graft config: %w", err)
	}

	tmpRepo := &repo.Repo{GraftDir: existingGraftDir}
	cfg, err := tmpRepo.ReadConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func gitBranchForReseed(ctx context.Context, rootDir, gitRef string) string {
	if strings.TrimSpace(gitRef) == "" || gitRef == "HEAD" {
		output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:branch", "symbolic-ref", "--quiet", "--short", "HEAD")
		if err == nil {
			return strings.TrimSpace(string(output))
		}
		return ""
	}
	if strings.HasPrefix(gitRef, "refs/heads/") {
		return strings.TrimPrefix(gitRef, "refs/heads/")
	}
	if err := runGitStreamingWithLabel(ctx, rootDir, io.Discard, io.Discard, "git-repair:show-ref", "show-ref", "--verify", "--quiet", "refs/heads/"+gitRef); err == nil {
		return gitRef
	}
	return ""
}

func gitCommitAuthor(ctx context.Context, rootDir, gitRef string) string {
	output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:author", "log", "-1", "--format=%an <%ae>", gitRef)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func reseedCommitMessage(ctx context.Context, rootDir, gitRef string) string {
	output, err := runGitCaptureWithLabel(ctx, rootDir, "git-repair:rev-parse", "rev-parse", "--short=12", gitRef)
	short := strings.TrimSpace(string(output))
	if err != nil || short == "" {
		return fmt.Sprintf("snapshot: reseed graft state from git %s", gitRef)
	}
	return fmt.Sprintf("snapshot: reseed graft state from git %s (%s)", gitRef, short)
}

func ensureBridgeScaffold(r *repo.Repo) error {
	for _, rel := range []string{
		filepath.Join("refs", "tags"),
		"info",
	} {
		if err := os.MkdirAll(filepath.Join(r.GraftDir, rel), 0o755); err != nil {
			return fmt.Errorf("create .graft/%s: %w", rel, err)
		}
	}
	return nil
}

func writeBridgeHashMap(r *repo.Repo) error {
	staging, err := r.ReadStaging()
	if err != nil {
		return err
	}
	hm, err := gitbridge.OpenHashMap(filepath.Join(r.GraftDir, "hashmap"))
	if err != nil {
		return err
	}
	defer hm.Close()

	for path, entry := range staging.Entries {
		content, err := os.ReadFile(filepath.Join(r.RootDir, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("read %s for hash map: %w", path, err)
		}
		if err := hm.Put(entry.BlobHash, gitbridge.GitObjectHash("blob", content)); err != nil {
			return fmt.Errorf("record hash map for %s: %w", path, err)
		}
	}
	return nil
}

func ensureGraftExcludedFromGit(rootDir string) error {
	infoDir := filepath.Join(rootDir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create .git/info: %w", err)
	}

	excludePath := filepath.Join(infoDir, "exclude")
	existing, _ := os.ReadFile(excludePath)
	if strings.Contains(string(existing), ".graft/") {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", excludePath, err)
	}
	defer f.Close()
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(".graft/\n")
	return err
}

func shortHashString(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

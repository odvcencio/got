package repo

import (
	"archive/tar"
	"archive/zip"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// ArchiveOptions configures archive creation.
type ArchiveOptions struct {
	Format string // "tar" or "zip" (default "tar")
	Prefix string // optional path prefix inside archive
}

// Archive resolves treeish to a commit, flattens its tree, and writes an
// archive (tar or zip) to w containing all files.
func (r *Repo) Archive(w io.Writer, treeish string, opts ArchiveOptions) error {
	format := strings.ToLower(opts.Format)
	if format == "" {
		format = "tar"
	}
	if format != "tar" && format != "zip" {
		return fmt.Errorf("archive: unsupported format %q (use tar or zip)", opts.Format)
	}

	commitHash, err := r.ResolveTreeish(treeish)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}

	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return fmt.Errorf("archive: read commit %s: %w", commitHash, err)
	}

	entries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("archive: flatten tree: %w", err)
	}

	// Sort entries by path for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	switch format {
	case "tar":
		return r.archiveTar(w, entries, opts.Prefix)
	case "zip":
		return r.archiveZip(w, entries, opts.Prefix)
	default:
		return fmt.Errorf("archive: unsupported format %q", format)
	}
}

func (r *Repo) archiveTar(w io.Writer, entries []TreeFileEntry, prefix string) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, entry := range entries {
		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			return fmt.Errorf("archive: read blob %s (%s): %w", entry.BlobHash, entry.Path, err)
		}

		name := entry.Path
		if prefix != "" {
			name = path.Join(prefix, name)
		}

		hdr := &tar.Header{
			Name: name,
			Mode: parseTarMode(entry.Mode),
			Size: int64(len(blob.Data)),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("archive: write tar header %s: %w", entry.Path, err)
		}
		if _, err := tw.Write(blob.Data); err != nil {
			return fmt.Errorf("archive: write tar data %s: %w", entry.Path, err)
		}
	}

	return nil
}

func (r *Repo) archiveZip(w io.Writer, entries []TreeFileEntry, prefix string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, entry := range entries {
		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			return fmt.Errorf("archive: read blob %s (%s): %w", entry.BlobHash, entry.Path, err)
		}

		name := entry.Path
		if prefix != "" {
			name = path.Join(prefix, name)
		}

		fh := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}

		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return fmt.Errorf("archive: create zip entry %s: %w", entry.Path, err)
		}
		if _, err := fw.Write(blob.Data); err != nil {
			return fmt.Errorf("archive: write zip data %s: %w", entry.Path, err)
		}
	}

	return nil
}

// parseTarMode converts a graft tree mode string to an int64 suitable for
// tar headers. Defaults to 0644 for regular files.
func parseTarMode(mode string) int64 {
	switch mode {
	case "100755":
		return 0o755
	case "100644", "":
		return 0o644
	default:
		return 0o644
	}
}

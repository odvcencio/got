package repo

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchive_Tar(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "hello.txt"), []byte("hello world\n"))
	writeFile(t, filepath.Join(dir, "sub", "deep.txt"), []byte("deep content\n"))
	if err := r.Add([]string{"hello.txt", "sub/deep.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var buf bytes.Buffer
	if err := r.Archive(&buf, string(commitHash), ArchiveOptions{Format: "tar"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Read back the tar.
	files := readTarFiles(t, &buf)
	if len(files) != 2 {
		t.Fatalf("got %d files in tar, want 2", len(files))
	}

	assertTarFile(t, files, "hello.txt", "hello world\n")
	assertTarFile(t, files, "sub/deep.txt", "deep content\n")
}

func TestArchive_Zip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "readme.txt"), []byte("readme contents\n"))
	if err := r.Add([]string{"readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var buf bytes.Buffer
	if err := r.Archive(&buf, string(commitHash), ArchiveOptions{Format: "zip"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Read back the zip.
	files := readZipFiles(t, buf.Bytes())
	if len(files) != 1 {
		t.Fatalf("got %d files in zip, want 1", len(files))
	}

	assertZipFile(t, files, "readme.txt", "readme contents\n")
}

func TestArchive_Prefix(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "data.txt"), []byte("data\n"))
	if err := r.Add([]string{"data.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("initial", "alice")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var buf bytes.Buffer
	if err := r.Archive(&buf, string(commitHash), ArchiveOptions{
		Format: "tar",
		Prefix: "myproject/",
	}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	files := readTarFiles(t, &buf)
	if len(files) != 1 {
		t.Fatalf("got %d files in tar, want 1", len(files))
	}

	// The file should be prefixed.
	if _, ok := files["myproject/data.txt"]; !ok {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		t.Fatalf("expected file myproject/data.txt in tar, got %v", names)
	}
}

func TestArchive_SpecificCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "f.txt"), []byte("version1\n"))
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	firstCommit, err := r.Commit("first", "alice")
	if err != nil {
		t.Fatalf("Commit first: %v", err)
	}

	// Create a second commit with different content.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("version2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"f.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("second", "bob"); err != nil {
		t.Fatalf("Commit second: %v", err)
	}

	// Archive the first commit specifically.
	var buf bytes.Buffer
	if err := r.Archive(&buf, string(firstCommit), ArchiveOptions{Format: "tar"}); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	files := readTarFiles(t, &buf)
	assertTarFile(t, files, "f.txt", "version1\n")
}

// --- helpers ---

func readTarFiles(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	files := make(map[string]string)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("readTarFiles: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("readTarFiles read %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(data)
	}
	return files
}

func assertTarFile(t *testing.T, files map[string]string, name, want string) {
	t.Helper()
	got, ok := files[name]
	if !ok {
		t.Fatalf("tar missing file %q", name)
	}
	if got != want {
		t.Fatalf("tar file %q = %q, want %q", name, got, want)
	}
}

func readZipFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("readZipFiles: %v", err)
	}
	files := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("readZipFiles open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("readZipFiles read %s: %v", f.Name, err)
		}
		files[f.Name] = string(body)
	}
	return files
}

func assertZipFile(t *testing.T, files map[string]string, name, want string) {
	t.Helper()
	got, ok := files[name]
	if !ok {
		t.Fatalf("zip missing file %q", name)
	}
	if got != want {
		t.Fatalf("zip file %q = %q, want %q", name, got, want)
	}
}

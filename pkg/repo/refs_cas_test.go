package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestUpdateRefCAS_ConcurrentSingleWinner(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	base := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := r.UpdateRef("refs/heads/main", base); err != nil {
		t.Fatalf("UpdateRef(base): %v", err)
	}

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)

	successCh := make(chan object.Hash, workers)
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			next := object.Hash(fmt.Sprintf("%064x", i+1))
			err := r.UpdateRefCAS("refs/heads/main", next, base)
			if err != nil {
				errCh <- err
				return
			}
			successCh <- next
		}()
	}

	wg.Wait()
	close(successCh)
	close(errCh)

	var winner object.Hash
	successes := 0
	for h := range successCh {
		successes++
		winner = h
	}
	if successes != 1 {
		t.Fatalf("successful CAS updates = %d, want 1", successes)
	}

	casMismatches := 0
	for err := range errCh {
		if errors.Is(err, ErrRefCASMismatch) {
			casMismatches++
			continue
		}
		t.Fatalf("unexpected error type: %v", err)
	}
	if casMismatches != workers-1 {
		t.Fatalf("CAS mismatches = %d, want %d", casMismatches, workers-1)
	}

	got, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}
	if got != winner {
		t.Fatalf("refs/heads/main = %s, want winner %s", got, winner)
	}
}

func TestUpdateRefCAS_CleansLockOnMismatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	current := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := r.UpdateRef("refs/heads/main", current); err != nil {
		t.Fatalf("UpdateRef(current): %v", err)
	}

	err = r.UpdateRefCAS(
		"refs/heads/main",
		object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
		object.Hash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
	)
	if !errors.Is(err, ErrRefCASMismatch) {
		t.Fatalf("expected CAS mismatch, got: %v", err)
	}

	lockPath := filepath.Join(r.GotDir, "refs", "heads", "main.lock")
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no lingering lockfile at %q, stat err=%v", lockPath, statErr)
	}
}

func TestCommitWithSigner_CASDetectsMovedBranchRef(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	if _, err := r.Commit("first commit", "test-author"); err != nil {
		t.Fatalf("Commit(first): %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { println(\"v2\") }\n"),
		0o644,
	); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go): %v", err)
	}

	movedHash := object.Hash("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	_, err := r.CommitWithSigner("second commit", "test-author", func(_ []byte) (string, error) {
		if err := r.UpdateRef("refs/heads/main", movedHash); err != nil {
			return "", err
		}
		return "signature", nil
	})
	if !errors.Is(err, ErrRefCASMismatch) {
		t.Fatalf("expected commit CAS mismatch, got: %v", err)
	}

	head, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}
	if head != movedHash {
		t.Fatalf("main ref = %s, want moved hash %s", head, movedHash)
	}
}

func TestCreateBranch_ConcurrentSingleWinner(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	headHash, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit(initial): %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	wg.Add(workers)

	successCh := make(chan struct{}, workers)
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if err := r.CreateBranch("feature", headHash); err != nil {
				errCh <- err
				return
			}
			successCh <- struct{}{}
		}()
	}

	wg.Wait()
	close(successCh)
	close(errCh)

	successes := len(successCh)
	if successes != 1 {
		t.Fatalf("CreateBranch successes = %d, want 1", successes)
	}

	duplicates := 0
	for err := range errCh {
		if strings.Contains(err.Error(), "already exists") {
			duplicates++
			continue
		}
		t.Fatalf("unexpected CreateBranch error: %v", err)
	}
	if duplicates != workers-1 {
		t.Fatalf("duplicate errors = %d, want %d", duplicates, workers-1)
	}

	got, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}
	if got != headHash {
		t.Fatalf("feature ref = %s, want %s", got, headHash)
	}
}

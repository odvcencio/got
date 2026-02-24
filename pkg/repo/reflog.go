package repo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

type ReflogEntry struct {
	Ref       string
	OldHash   object.Hash
	NewHash   object.Hash
	Timestamp int64
	Reason    string
}

func (r *Repo) appendReflog(ref string, oldHash, newHash object.Hash, reason string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "update"
	}

	logPath := filepath.Join(r.GotDir, "logs", filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("reflog mkdir: %w", err)
	}

	old := string(oldHash)
	if strings.TrimSpace(old) == "" {
		old = zeroHash
	}
	newVal := string(newHash)
	if strings.TrimSpace(newVal) == "" {
		newVal = zeroHash
	}
	line := fmt.Sprintf("%s %s %d %s\n", old, newVal, time.Now().Unix(), reason)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("reflog open: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("reflog write: %w", err)
	}
	return nil
}

func (r *Repo) ReadReflog(ref string, limit int) ([]ReflogEntry, error) {
	refName, err := r.resolveReflogRefName(ref)
	if err != nil {
		return nil, err
	}

	logPath := filepath.Join(r.GotDir, "logs", filepath.FromSlash(refName))
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read reflog: %w", err)
	}
	defer f.Close()

	var entries []ReflogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, ReflogEntry{
			Ref:       refName,
			OldHash:   object.Hash(parts[0]),
			NewHash:   object.Hash(parts[1]),
			Timestamp: ts,
			Reason:    parts[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read reflog: %w", err)
	}

	// Return newest first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (r *Repo) resolveReflogRefName(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "HEAD" {
		head, err := r.Head()
		if err == nil && strings.HasPrefix(head, "refs/") {
			return head, nil
		}
		return "HEAD", nil
	}
	if strings.HasPrefix(ref, "refs/") {
		return ref, nil
	}
	return "refs/heads/" + ref, nil
}

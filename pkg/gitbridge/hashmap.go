package gitbridge

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/odvcencio/graft/pkg/object"
)

// GitHash is a variable-length git object hash (SHA-1: 20 bytes, SHA-256: 32 bytes).
type GitHash []byte

func (h GitHash) Hex() string { return hex.EncodeToString(h) }

// ParseGitHash parses a hex-encoded git hash.
func ParseGitHash(s string) (GitHash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid git hash %q: %w", s, err)
	}
	return GitHash(b), nil
}

// HashMap provides bidirectional mapping between graft and git hashes.
type HashMap struct {
	mu      sync.RWMutex
	path    string
	file    *os.File
	toGit   map[object.Hash]GitHash
	toGraft map[string]object.Hash // keyed by hex(gitHash)
}

// OpenHashMap opens or creates a hash map file.
func OpenHashMap(path string) (*HashMap, error) {
	hm := &HashMap{
		path:    path,
		toGit:   make(map[object.Hash]GitHash),
		toGraft: make(map[string]object.Hash),
	}

	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				continue
			}
			graftHash := object.Hash(parts[0])
			gitHash, err := ParseGitHash(parts[1])
			if err != nil {
				continue
			}
			hm.toGit[graftHash] = gitHash
			hm.toGraft[gitHash.Hex()] = graftHash
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open hash map: %w", err)
	}
	hm.file = f
	return hm, nil
}

func (hm *HashMap) GraftToGit(graftHash object.Hash) (GitHash, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	h, ok := hm.toGit[graftHash]
	return h, ok
}

func (hm *HashMap) GitToGraft(gitHash GitHash) (object.Hash, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	h, ok := hm.toGraft[gitHash.Hex()]
	return h, ok
}

func (hm *HashMap) Put(graftHash object.Hash, gitHash GitHash) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.toGit[graftHash] = gitHash
	hm.toGraft[gitHash.Hex()] = graftHash
	_, err := fmt.Fprintf(hm.file, "%s %s\n", string(graftHash), gitHash.Hex())
	return err
}

func (hm *HashMap) Close() error {
	if hm.file != nil {
		return hm.file.Close()
	}
	return nil
}

func (hm *HashMap) Len() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.toGit)
}

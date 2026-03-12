//go:build grammar_blobs_external && (linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package grammars

import (
	"os"
	"strconv"
	"syscall"
)

func readGrammarBlobFromPath(path string) (grammarBlob, error) {
	if raw := os.Getenv("GOTREESITTER_GRAMMAR_BLOB_MMAP"); raw != "" {
		if enabled, err := strconv.ParseBool(raw); err == nil && !enabled {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return grammarBlob{}, rerr
			}
			return grammarBlob{data: data}, nil
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return grammarBlob{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return grammarBlob{}, err
	}
	size := fi.Size()
	if size <= 0 {
		return grammarBlob{data: nil}, nil
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		// Fallback to regular reads if mmap is not available in this runtime.
		fallback, rerr := os.ReadFile(path)
		if rerr != nil {
			return grammarBlob{}, err
		}
		return grammarBlob{data: fallback}, nil
	}

	return grammarBlob{
		data: data,
		release: func() {
			_ = syscall.Munmap(data)
		},
	}, nil
}

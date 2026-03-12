//go:build grammar_blobs_external && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package grammars

import "os"

func readGrammarBlobFromPath(path string) (grammarBlob, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return grammarBlob{}, err
	}
	return grammarBlob{data: data}, nil
}

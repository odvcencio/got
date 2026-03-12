//go:build grammar_blobs_external

package grammars

import (
	"os"
	"path/filepath"
)

func readGrammarBlob(blobName string) (grammarBlob, error) {
	root := os.Getenv("GOTREESITTER_GRAMMAR_BLOB_DIR")
	if root == "" {
		root = filepath.Join("grammars", "grammar_blobs")
	}
	path := filepath.Join(root, blobName)
	return readGrammarBlobFromPath(path)
}

package repo

import "github.com/odvcencio/got/pkg/object"

// Repo represents an opened Got repository.
type Repo struct {
	RootDir string        // working directory root
	GotDir  string        // .got/ directory
	Store   *object.Store // content-addressed object store
}

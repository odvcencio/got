package coord

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
)

// PeerTransport abstracts reading coordination state from a peer workspace.
type PeerTransport interface {
	// ListAgents returns agents registered in the peer workspace.
	ListAgents() ([]AgentInfo, error)
	// ListClaims returns claims in the peer workspace.
	ListClaims() ([]ClaimInfo, error)
	// ReadExportIndex returns the peer's export index.
	ReadExportIndex() (*ExportIndex, error)
	// ReadXrefIndex returns the peer's xref index.
	ReadXrefIndex() (*XrefIndex, error)
}

// LocalPeerTransport reads coordination state from another workspace's
// .graft/ directory on the local filesystem using repo.Open.
type LocalPeerTransport struct {
	Path string // absolute path to the peer workspace root
}

// NewLocalPeerTransport creates a LocalPeerTransport for the given workspace path.
func NewLocalPeerTransport(path string) *LocalPeerTransport {
	return &LocalPeerTransport{Path: path}
}

func (t *LocalPeerTransport) openCoordinator() (*Coordinator, error) {
	r, err := repo.Open(t.Path)
	if err != nil {
		return nil, fmt.Errorf("open peer repo at %s: %w", t.Path, err)
	}
	return New(r, DefaultConfig), nil
}

// ListAgents returns agents registered in the peer workspace.
func (t *LocalPeerTransport) ListAgents() ([]AgentInfo, error) {
	c, err := t.openCoordinator()
	if err != nil {
		return nil, err
	}
	return c.ListAgents()
}

// ListClaims returns claims in the peer workspace.
func (t *LocalPeerTransport) ListClaims() ([]ClaimInfo, error) {
	c, err := t.openCoordinator()
	if err != nil {
		return nil, err
	}
	return c.ListClaims()
}

// ReadExportIndex returns the peer's export index.
func (t *LocalPeerTransport) ReadExportIndex() (*ExportIndex, error) {
	c, err := t.openCoordinator()
	if err != nil {
		return nil, err
	}
	return c.LoadExportIndex()
}

// ReadXrefIndex returns the peer's xref index.
func (t *LocalPeerTransport) ReadXrefIndex() (*XrefIndex, error) {
	c, err := t.openCoordinator()
	if err != nil {
		return nil, err
	}
	return c.LoadXrefIndex()
}

// RemotePeerTransport is a stub for fetching coordination state from a remote
// peer over the graft protocol. This will be implemented when remote
// coordination federation is needed.
type RemotePeerTransport struct {
	RemoteURL string
}

// NewRemotePeerTransport creates a RemotePeerTransport for the given remote URL.
func NewRemotePeerTransport(url string) *RemotePeerTransport {
	return &RemotePeerTransport{RemoteURL: url}
}

var errRemoteNotImplemented = fmt.Errorf("remote peer transport not yet implemented")

// ListAgents returns an error indicating remote transport is not yet implemented.
func (t *RemotePeerTransport) ListAgents() ([]AgentInfo, error) {
	return nil, errRemoteNotImplemented
}

// ListClaims returns an error indicating remote transport is not yet implemented.
func (t *RemotePeerTransport) ListClaims() ([]ClaimInfo, error) {
	return nil, errRemoteNotImplemented
}

// ReadExportIndex returns an error indicating remote transport is not yet implemented.
func (t *RemotePeerTransport) ReadExportIndex() (*ExportIndex, error) {
	return nil, errRemoteNotImplemented
}

// ReadXrefIndex returns an error indicating remote transport is not yet implemented.
func (t *RemotePeerTransport) ReadXrefIndex() (*XrefIndex, error) {
	return nil, errRemoteNotImplemented
}

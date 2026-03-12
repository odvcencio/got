package coord

import (
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestLocalPeerTransport_ListAgents(t *testing.T) {
	// Create two repos: one as the "peer" with coord state, one as "local"
	peerDir := t.TempDir()
	peerRepo, err := repo.Init(peerDir)
	if err != nil {
		t.Fatalf("Init peer repo: %v", err)
	}

	// Set up coordination state in the peer repo
	peerCoord := New(peerRepo, DefaultConfig)
	_, err = peerCoord.RegisterAgent(AgentInfo{
		Name:      "peer-agent",
		Workspace: "peer-workspace",
		Host:      "peer-host",
	})
	if err != nil {
		t.Fatalf("RegisterAgent in peer: %v", err)
	}

	// Use LocalPeerTransport to read from the peer repo
	transport := NewLocalPeerTransport(peerDir)

	agents, err := transport.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents via transport: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent from peer, got %d", len(agents))
	}
	if agents[0].Name != "peer-agent" {
		t.Errorf("agent name = %q, want peer-agent", agents[0].Name)
	}
}

func TestLocalPeerTransport_ListClaims(t *testing.T) {
	peerDir := t.TempDir()
	peerRepo, err := repo.Init(peerDir)
	if err != nil {
		t.Fatalf("Init peer repo: %v", err)
	}

	peerCoord := New(peerRepo, DefaultConfig)
	id, err := peerCoord.RegisterAgent(AgentInfo{
		Name:      "peer-agent",
		Workspace: "peer-workspace",
		Host:      "peer-host",
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	err = peerCoord.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::PeerFunc:func PeerFunc():0",
		File:      "peer.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	transport := NewLocalPeerTransport(peerDir)

	claims, err := transport.ListClaims()
	if err != nil {
		t.Fatalf("ListClaims via transport: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim from peer, got %d", len(claims))
	}
	if claims[0].AgentName != "peer-agent" {
		t.Errorf("claim agent = %q, want peer-agent", claims[0].AgentName)
	}
}

func TestLocalPeerTransport_ExportIndex(t *testing.T) {
	peerDir := t.TempDir()
	peerRepo, err := repo.Init(peerDir)
	if err != nil {
		t.Fatalf("Init peer repo: %v", err)
	}

	peerCoord := New(peerRepo, DefaultConfig)

	// Save an export index
	idx := &ExportIndex{
		Packages: map[string]map[string]ExportedEntity{
			"pkg/api": {
				"func:HandleRequest": {
					Key:       "func:HandleRequest",
					Signature: "func HandleRequest()",
					File:      "api.go",
				},
			},
		},
	}
	if err := peerCoord.SaveExportIndex(idx); err != nil {
		t.Fatalf("SaveExportIndex: %v", err)
	}

	transport := NewLocalPeerTransport(peerDir)

	readIdx, err := transport.ReadExportIndex()
	if err != nil {
		t.Fatalf("ReadExportIndex via transport: %v", err)
	}
	if readIdx == nil {
		t.Fatal("expected non-nil export index")
	}
	if len(readIdx.Packages) != 1 {
		t.Fatalf("expected 1 package in export index, got %d", len(readIdx.Packages))
	}
	pkgEntities, ok := readIdx.Packages["pkg/api"]
	if !ok {
		t.Fatal("expected pkg/api in export index")
	}
	if _, ok := pkgEntities["func:HandleRequest"]; !ok {
		t.Error("expected func:HandleRequest in export index")
	}
}

func TestLocalPeerTransport_XrefIndex(t *testing.T) {
	peerDir := t.TempDir()
	peerRepo, err := repo.Init(peerDir)
	if err != nil {
		t.Fatalf("Init peer repo: %v", err)
	}

	peerCoord := New(peerRepo, DefaultConfig)

	// Save a xref index
	idx := &XrefIndex{
		Refs: map[string][]XrefCallSite{
			"github.com/example/lib.Parse": {
				{File: "handler.go", Entity: "HandleRequest", Line: 42},
			},
		},
	}
	if err := peerCoord.SaveXrefIndex(idx); err != nil {
		t.Fatalf("SaveXrefIndex: %v", err)
	}

	transport := NewLocalPeerTransport(peerDir)

	readIdx, err := transport.ReadXrefIndex()
	if err != nil {
		t.Fatalf("ReadXrefIndex via transport: %v", err)
	}
	if readIdx == nil {
		t.Fatal("expected non-nil xref index")
	}
	sites, ok := readIdx.Refs["github.com/example/lib.Parse"]
	if !ok {
		t.Fatal("expected xref entry")
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 call site, got %d", len(sites))
	}
	if sites[0].Entity != "HandleRequest" {
		t.Errorf("call site entity = %q, want HandleRequest", sites[0].Entity)
	}
}

func TestRemotePeerTransport_ReturnsNotImplemented(t *testing.T) {
	transport := NewRemotePeerTransport("https://example.com/repo.git")

	if _, err := transport.ListAgents(); err == nil {
		t.Error("expected not-implemented error from ListAgents")
	}
	if _, err := transport.ListClaims(); err == nil {
		t.Error("expected not-implemented error from ListClaims")
	}
	if _, err := transport.ReadExportIndex(); err == nil {
		t.Error("expected not-implemented error from ReadExportIndex")
	}
	if _, err := transport.ReadXrefIndex(); err == nil {
		t.Error("expected not-implemented error from ReadXrefIndex")
	}
}

// Verify both transport types satisfy the PeerTransport interface.
var _ PeerTransport = (*LocalPeerTransport)(nil)
var _ PeerTransport = (*RemotePeerTransport)(nil)

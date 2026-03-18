package main

import (
	"bytes"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestHandleCoordAddClaim_ForceTransfersClaim(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	c.Config.ConflictMode = "soft_block"

	ownerID, err := c.RegisterAgent(coord.AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent owner: %v", err)
	}
	forcerID, err := c.RegisterAgent(coord.AgentInfo{Name: "forcer", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent forcer: %v", err)
	}

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::Takeover:func Takeover():0",
		File:      "takeover.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(ownerID, req); err != nil {
		t.Fatalf("AcquireClaim owner: %v", err)
	}

	var out bytes.Buffer
	if err := handleCoordAddClaim(c, &out, forcerID, req, true); err != nil {
		t.Fatalf("handleCoordAddClaim: %v", err)
	}

	claim, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		t.Fatalf("LoadClaim: %v", err)
	}
	if claim == nil {
		t.Fatal("expected active claim after force transfer")
	}
	if claim.Agent != forcerID {
		t.Fatalf("claim.Agent = %q, want %q", claim.Agent, forcerID)
	}

	decisions, err := coord.ListDecisions(r.GraftDir, 10)
	if err != nil {
		t.Fatalf("coord.ListDecisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("expected recorded force decision")
	}
	if decisions[0].Outcome.Status != "force_transferred" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want force_transferred", decisions[0].Outcome.Status)
	}
	if !decisions[0].Outcome.ClaimTransferred {
		t.Fatal("expected force decision to mark claim transfer")
	}
	if decisions[0].Outcome.TransferredFromID != ownerID {
		t.Fatalf("TransferredFromID = %q, want %q", decisions[0].Outcome.TransferredFromID, ownerID)
	}
	if got := out.String(); got == "" {
		t.Fatal("expected force transfer output")
	}
}

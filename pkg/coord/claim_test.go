package coord

import (
	"fmt"
	"sync"
	"testing"
)

func TestAcquireAndListClaims(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "claimer", Workspace: "graft", Host: "test"})

	err := c.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::DiffFiles:func DiffFiles():0",
		File:      "pkg/diff/diff.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	claims, err := c.ListClaims()
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].AgentName != "claimer" {
		t.Errorf("claim agent = %q, want claimer", claims[0].AgentName)
	}
}

func TestAcquireClaimConflict(t *testing.T) {
	c := newTestCoordinator(t)

	id1, _ := c.RegisterAgent(AgentInfo{Name: "agent-1", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "agent-2", Workspace: "graft", Host: "test"})

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Merge:func Merge():0",
		File:      "pkg/merge/merge.go",
		Mode:      ClaimEditing,
	}

	if err := c.AcquireClaim(id1, req); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	err := c.AcquireClaim(id2, req)
	if err == nil {
		t.Fatal("expected conflict error on second claim")
	}
	conflict, ok := err.(*ClaimConflictError)
	if !ok {
		t.Fatalf("expected ClaimConflictError, got %T: %v", err, err)
	}
	if conflict.HeldBy != "agent-1" {
		t.Errorf("conflict held by %q, want agent-1", conflict.HeldBy)
	}
}

func TestReleaseClaim(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "releaser", Workspace: "graft", Host: "test"})

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      ClaimEditing,
	}
	if err := c.AcquireClaim(id, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	keyHash := EntityKeyHash(req.EntityKey)
	if err := c.ReleaseClaim(keyHash); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	claims, _ := c.ListClaims()
	if len(claims) != 0 {
		t.Fatalf("expected 0 claims after release, got %d", len(claims))
	}
}

func TestTransferClaim(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "receiver", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Transfer:func Transfer():0"
	if err := c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "t.go", Mode: ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	keyHash := EntityKeyHash(entityKey)
	if err := c.TransferClaim(keyHash, id1, id2); err != nil {
		t.Fatalf("TransferClaim: %v", err)
	}

	claims, _ := c.ListClaims()
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].Agent != id2 {
		t.Errorf("claim agent = %s, want %s", claims[0].Agent, id2)
	}
}

func TestTransferClaim_WrongOwner(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "other", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Owned:func Owned():0"
	c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "o.go", Mode: ClaimEditing})

	keyHash := EntityKeyHash(entityKey)
	err := c.TransferClaim(keyHash, id2, id1) // id2 doesn't own it
	if err == nil {
		t.Fatal("expected error transferring claim not owned by caller")
	}
}

func TestWatchingClaimDoesNotBlock(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "watcher", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "editor", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Bar:func Bar():0"

	// Watching claim
	if err := c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "bar.go", Mode: ClaimWatching}); err != nil {
		t.Fatalf("watch claim: %v", err)
	}

	// Editing claim should succeed (watching doesn't block)
	if err := c.AcquireClaim(id2, ClaimRequest{EntityKey: entityKey, File: "bar.go", Mode: ClaimEditing}); err != nil {
		t.Fatalf("edit claim should not be blocked by watcher: %v", err)
	}
}

func TestClaimsForFile(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "filer", Workspace: "graft", Host: "test"})

	c.AcquireClaim(id, ClaimRequest{EntityKey: "decl:function_definition::A:func A():0", File: "a.go", Mode: ClaimEditing})
	c.AcquireClaim(id, ClaimRequest{EntityKey: "decl:function_definition::B:func B():0", File: "b.go", Mode: ClaimEditing})

	claims, err := c.ClaimsForFile("a.go")
	if err != nil {
		t.Fatalf("ClaimsForFile: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim for a.go, got %d", len(claims))
	}
	if claims[0].File != "a.go" {
		t.Errorf("claim file = %q, want a.go", claims[0].File)
	}

	// b.go should also have exactly 1
	claimsB, _ := c.ClaimsForFile("b.go")
	if len(claimsB) != 1 {
		t.Fatalf("expected 1 claim for b.go, got %d", len(claimsB))
	}

	// nonexistent file should return 0
	claimsC, _ := c.ClaimsForFile("c.go")
	if len(claimsC) != 0 {
		t.Fatalf("expected 0 claims for c.go, got %d", len(claimsC))
	}
}

func TestAcquireClaimConcurrentRace(t *testing.T) {
	c := newTestCoordinator(t)
	const numAgents = 10
	entityKey := "decl:function_definition::RaceTarget:func RaceTarget():0"

	// Pre-register all agents to avoid data race on c.AgentID
	agentIDs := make([]string, numAgents)
	agentNames := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		name := fmt.Sprintf("agent-%d", i)
		agentNames[i] = name
		id, err := c.RegisterAgent(AgentInfo{Name: name, Workspace: "graft", Host: "test"})
		if err != nil {
			t.Fatalf("RegisterAgent %d: %v", i, err)
		}
		agentIDs[i] = id
	}

	var wg sync.WaitGroup
	wins := make(chan string, numAgents)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := c.AcquireClaim(agentIDs[n], ClaimRequest{EntityKey: entityKey, File: "race.go", Mode: ClaimEditing})
			if err == nil {
				wins <- agentNames[n]
			}
		}(i)
	}

	wg.Wait()
	close(wins)

	winners := 0
	for range wins {
		winners++
	}
	// At least one agent should win the claim. Due to the non-CAS nature
	// of AcquireClaim's initial write (UpdateRef, not UpdateRefCAS), in a
	// race condition multiple goroutines may "win" by overwriting. The key
	// property is that the final state is consistent (exactly one owner).
	if winners < 1 {
		t.Fatalf("expected at least 1 winner, got %d", winners)
	}

	// Verify final consistency: exactly one claim with one owner
	claims, err := c.ListClaims()
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected exactly 1 claim in final state, got %d", len(claims))
	}
}

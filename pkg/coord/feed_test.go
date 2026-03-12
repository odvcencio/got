package coord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAppendAndWalkFeed(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "feeder", Workspace: "graft", Host: "test"})

	event1 := FeedEvent{
		Event:     "entity_changed",
		AgentID:   id,
		AgentName: "feeder",
		Entities: []EntityChange{
			{Key: "decl:function_definition::Foo:func Foo():0", File: "foo.go", Change: "body_changed"},
		},
	}
	if err := c.AppendFeed(event1); err != nil {
		t.Fatalf("AppendFeed 1: %v", err)
	}

	event2 := FeedEvent{
		Event:     "entity_changed",
		AgentID:   id,
		AgentName: "feeder",
		Entities: []EntityChange{
			{Key: "decl:function_definition::Bar:func Bar():0", File: "bar.go", Change: "signature_changed"},
		},
	}
	if err := c.AppendFeed(event2); err != nil {
		t.Fatalf("AppendFeed 2: %v", err)
	}

	events, err := c.WalkFeed("", 10)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Events are newest-first (head of chain)
	if events[0].Entities[0].File != "bar.go" {
		t.Errorf("first event file = %q, want bar.go", events[0].Entities[0].File)
	}
}

func TestWalkFeedSince(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "feeder", Workspace: "graft", Host: "test"})

	// Append 3 events
	for _, name := range []string{"A", "B", "C"} {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "feeder",
			Entities: []EntityChange{{Key: name, File: name + ".go", Change: "body_changed"}},
		})
	}

	// Get all, grab the hash of event B (second from head = index 1)
	all, _ := c.WalkFeed("", 10)
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// Walk since B's blob hash -- should only return C (newer than B)
	sinceHash := all[1].FeedHash
	recent, err := c.WalkFeed(sinceHash, 10)
	if err != nil {
		t.Fatalf("WalkFeed since: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 event since B, got %d", len(recent))
	}
}

func TestAppendFeedConcurrentRetry(t *testing.T) {
	c := newTestCoordinator(t)
	const numAppends = 20

	var wg sync.WaitGroup
	for i := 0; i < numAppends; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.AppendFeed(FeedEvent{
				Event:     "entity_changed",
				AgentID:   fmt.Sprintf("agent-%d", n),
				AgentName: fmt.Sprintf("agent-%d", n),
				Entities:  []EntityChange{{Key: fmt.Sprintf("entity-%d", n), File: "test.go", Change: "body_changed"}},
			})
		}(i)
	}
	wg.Wait()

	events, err := c.WalkFeed("", 100)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	// All events should be present in the chain or overflow.
	// Due to CAS retry with 5 attempts, most should land in the chain.
	// With overflow, none are dropped.
	if len(events) < numAppends/2 {
		t.Fatalf("expected most events to land, got %d/%d", len(events), numAppends)
	}
	t.Logf("landed %d/%d events in feed chain", len(events), numAppends)
}

// --- Task 20: Feed cursor management tests ---

func TestCursorSaveLoad(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "cursor-agent", Workspace: "graft", Host: "test"})

	// Append some events
	for i := 0; i < 3; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "cursor-agent",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	events, _ := c.WalkFeed("", 10)
	cursorHash := events[0].FeedHash // newest event

	if err := c.SaveCursor(id, cursorHash); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}

	loaded, err := c.LoadCursor(id)
	if err != nil {
		t.Fatalf("LoadCursor: %v", err)
	}
	if loaded != cursorHash {
		t.Errorf("cursor = %q, want %q", loaded, cursorHash)
	}
}

func TestWalkFeedSinceCursor(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "cursor-agent", Workspace: "graft", Host: "test"})

	// Append 3 events, save cursor after 2nd
	for i := 0; i < 3; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "cursor-agent",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	events, _ := c.WalkFeed("", 10)
	// Save cursor at the 2nd event (index 1, middle)
	c.SaveCursor(id, events[1].FeedHash)

	// WalkFeedSinceCursor should only return event 0 (newest, after cursor)
	recent, err := c.WalkFeedSinceCursor(id, 10)
	if err != nil {
		t.Fatalf("WalkFeedSinceCursor: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 event since cursor, got %d", len(recent))
	}
}

// --- Task 21: Feed pruning tests ---

func TestPruneFeed(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "pruner", Workspace: "graft", Host: "test"})

	// Append 5 events
	for i := 0; i < 5; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "pruner",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	// Prune keeping only the 2 newest
	pruned, err := c.PruneFeed(2)
	if err != nil {
		t.Fatalf("PruneFeed: %v", err)
	}
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}

	// Walk should return only 2
	events, _ := c.WalkFeed("", 10)
	if len(events) != 2 {
		t.Fatalf("expected 2 events after prune, got %d", len(events))
	}
}

func TestPruneFeed_ResetsStaleCursors(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "stale-cursor", Workspace: "graft", Host: "test"})

	// Append 5 events, save cursor at oldest
	for i := 0; i < 5; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "stale-cursor",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}
	all, _ := c.WalkFeed("", 10)
	c.SaveCursor(id, all[4].FeedHash) // oldest

	// Prune keeps 2 — cursor now points to pruned range
	c.PruneFeed(2)

	// WalkFeedSinceCursor should gracefully return all available events
	events, err := c.WalkFeedSinceCursor(id, 10)
	if err != nil {
		t.Fatalf("WalkFeedSinceCursor after prune: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (full walk after stale cursor), got %d", len(events))
	}
}

// --- Task 22: Feed overflow merge tests ---

func TestOverflowMerge(t *testing.T) {
	c := newTestCoordinator(t)

	// Manually write overflow files
	overflowDir := filepath.Join(c.Repo.GraftDir, "coord", "feed-overflow")
	os.MkdirAll(overflowDir, 0o755)
	for i := 0; i < 3; i++ {
		evt := FeedEvent{
			Event: "entity_changed", AgentID: "overflow-agent", AgentName: "overflow",
			Entities: []EntityChange{{Key: fmt.Sprintf("overflow-%d", i), File: "o.go", Change: "body_changed"}},
		}
		data, _ := json.Marshal(evt)
		os.WriteFile(filepath.Join(overflowDir, fmt.Sprintf("%d-overflow.json", i)), data, 0o644)
	}

	// Merge overflow into feed
	merged, err := c.MergeOverflow()
	if err != nil {
		t.Fatalf("MergeOverflow: %v", err)
	}
	if merged != 3 {
		t.Errorf("expected 3 merged, got %d", merged)
	}

	// Feed should now have 3 events
	events, _ := c.WalkFeed("", 10)
	if len(events) != 3 {
		t.Fatalf("expected 3 events after merge, got %d", len(events))
	}

	// Overflow directory should be empty
	entries, _ := os.ReadDir(overflowDir)
	if len(entries) != 0 {
		t.Errorf("expected overflow dir to be empty, got %d files", len(entries))
	}
}

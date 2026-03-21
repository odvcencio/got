package main

import (
	"testing"
	"time"
)

func TestAccumulator_RecordAndDigest(t *testing.T) {
	acc := newMCPActivityAccumulator("agent1", "cedar")

	acc.recordToolCall("graft_coord_agents")
	acc.recordFileRead("pkg/coord/agent.go")
	acc.recordFileRead("pkg/coord/feed.go")
	acc.recordFileWrite("pkg/coord/claim.go")
	acc.recordBlocked()
	acc.recordAdvisory()

	digest := acc.buildDigest()
	if digest.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", digest.ToolCalls)
	}
	if len(digest.FilesRead) != 2 {
		t.Errorf("FilesRead = %d, want 2", len(digest.FilesRead))
	}
	if len(digest.FilesWritten) != 1 {
		t.Errorf("FilesWritten = %d, want 1", len(digest.FilesWritten))
	}
	if digest.Blocked != 1 {
		t.Errorf("Blocked = %d, want 1", digest.Blocked)
	}
	if digest.Advisories != 1 {
		t.Errorf("Advisories = %d, want 1", digest.Advisories)
	}
}

func TestAccumulator_ShouldPublish(t *testing.T) {
	acc := newMCPActivityAccumulator("agent1", "cedar")
	if acc.shouldPublish() {
		t.Error("should not publish immediately after creation")
	}
	acc.mu.Lock()
	acc.lastPublished = time.Now().Add(-31 * time.Second)
	acc.mu.Unlock()
	if !acc.shouldPublish() {
		t.Error("should publish after 30s")
	}
}

func TestAccumulator_Reset(t *testing.T) {
	acc := newMCPActivityAccumulator("agent1", "cedar")
	acc.recordToolCall("foo")
	acc.recordFileRead("bar.go")
	acc.recordBlocked()
	acc.reset()
	digest := acc.buildDigest()
	if digest.ToolCalls != 0 {
		t.Errorf("ToolCalls after reset = %d, want 0", digest.ToolCalls)
	}
	if len(digest.FilesRead) != 0 {
		t.Errorf("FilesRead after reset = %d, want 0", len(digest.FilesRead))
	}
	if digest.Blocked != 0 {
		t.Errorf("Blocked after reset = %d, want 0", digest.Blocked)
	}
}

func TestAccumulator_Snapshot(t *testing.T) {
	acc := newMCPActivityAccumulator("agent1", "cedar")
	acc.recordToolCall("foo")
	acc.recordFileRead("bar.go")
	acc.recordFileWrite("baz.go")

	snap := acc.snapshot()
	if snap["tool_calls"] != 1 {
		t.Errorf("tool_calls = %v, want 1", snap["tool_calls"])
	}
	if snap["files_read"] != 1 {
		t.Errorf("files_read = %v, want 1", snap["files_read"])
	}
	if snap["files_written"] != 1 {
		t.Errorf("files_written = %v, want 1", snap["files_written"])
	}
}

func TestAccumulator_DedupFiles(t *testing.T) {
	acc := newMCPActivityAccumulator("agent1", "cedar")
	acc.recordFileRead("pkg/foo.go")
	acc.recordFileRead("pkg/foo.go") // duplicate
	acc.recordFileRead("pkg/bar.go")

	digest := acc.buildDigest()
	if len(digest.FilesRead) != 2 {
		t.Errorf("FilesRead = %d, want 2 (should dedup)", len(digest.FilesRead))
	}
}

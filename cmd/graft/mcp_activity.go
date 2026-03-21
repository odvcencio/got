package main

import (
	"sync"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
)

const digestInterval = 30 * time.Second

type mcpActivityAccumulator struct {
	mu            sync.Mutex
	toolCalls     int
	filesRead     map[string]struct{}
	filesWritten  map[string]struct{}
	blocked       int
	advisories    int
	lastPublished time.Time
	agentID       string
	agentName     string
}

func newMCPActivityAccumulator(agentID, agentName string) *mcpActivityAccumulator {
	return &mcpActivityAccumulator{
		filesRead:     make(map[string]struct{}),
		filesWritten:  make(map[string]struct{}),
		lastPublished: time.Now(),
		agentID:       agentID,
		agentName:     agentName,
	}
}

func (a *mcpActivityAccumulator) recordToolCall(toolName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCalls++
}

func (a *mcpActivityAccumulator) recordFileRead(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.filesRead[path] = struct{}{}
}

func (a *mcpActivityAccumulator) recordFileWrite(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.filesWritten[path] = struct{}{}
}

func (a *mcpActivityAccumulator) recordBlocked() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocked++
}

func (a *mcpActivityAccumulator) recordAdvisory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.advisories++
}

func (a *mcpActivityAccumulator) shouldPublish() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return time.Since(a.lastPublished) >= digestInterval
}

func (a *mcpActivityAccumulator) buildDigest() *coord.ActivityDigest {
	a.mu.Lock()
	defer a.mu.Unlock()

	reads := make([]string, 0, len(a.filesRead))
	for f := range a.filesRead {
		reads = append(reads, f)
	}
	writes := make([]string, 0, len(a.filesWritten))
	for f := range a.filesWritten {
		writes = append(writes, f)
	}

	// ActiveFiles: top 5 most accessed (union of reads + writes)
	allFiles := make(map[string]struct{})
	for f := range a.filesRead {
		allFiles[f] = struct{}{}
	}
	for f := range a.filesWritten {
		allFiles[f] = struct{}{}
	}
	active := make([]string, 0, 5)
	for f := range allFiles {
		active = append(active, f)
		if len(active) >= 5 {
			break
		}
	}

	return &coord.ActivityDigest{
		ToolCalls:    a.toolCalls,
		FilesRead:    reads,
		FilesWritten: writes,
		ActiveFiles:  active,
		Period:       int(time.Since(a.lastPublished).Seconds()),
		Blocked:      a.blocked,
		Advisories:   a.advisories,
	}
}

func (a *mcpActivityAccumulator) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolCalls = 0
	a.filesRead = make(map[string]struct{})
	a.filesWritten = make(map[string]struct{})
	a.blocked = 0
	a.advisories = 0
	a.lastPublished = time.Now()
}

func (a *mcpActivityAccumulator) snapshot() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]any{
		"tool_calls":           a.toolCalls,
		"files_read":           len(a.filesRead),
		"files_written":        len(a.filesWritten),
		"seconds_since_digest": int(time.Since(a.lastPublished).Seconds()),
	}
}

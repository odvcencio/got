package remote

import (
	"bytes"
	"io"
	"testing"
)

func TestSidebandRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)

	if err := sw.WriteData([]byte("pack-data-1")); err != nil {
		t.Fatalf("WriteData: %v", err)
	}
	if err := sw.WriteProgress("50%%"); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
	if err := sw.WriteData([]byte("pack-data-2")); err != nil {
		t.Fatalf("WriteData: %v", err)
	}

	sr := NewSidebandReader(&buf)
	var dataFrames [][]byte
	var progressFrames []string

	for {
		channel, payload, err := sr.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		switch channel {
		case SidebandData:
			dataFrames = append(dataFrames, append([]byte{}, payload...))
		case SidebandProgress:
			progressFrames = append(progressFrames, string(payload))
		}
	}

	if len(dataFrames) != 2 {
		t.Fatalf("data frames: %d, want 2", len(dataFrames))
	}
	if string(dataFrames[0]) != "pack-data-1" {
		t.Fatalf("data[0] = %q", dataFrames[0])
	}
	if string(dataFrames[1]) != "pack-data-2" {
		t.Fatalf("data[1] = %q", dataFrames[1])
	}
	if len(progressFrames) != 1 || progressFrames[0] != "50%%" {
		t.Fatalf("progress = %v", progressFrames)
	}
}

func TestSidebandErrorFrame(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	if err := sw.WriteError("disk full"); err != nil {
		t.Fatalf("WriteError: %v", err)
	}

	sr := NewSidebandReader(&buf)
	channel, payload, err := sr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if channel != SidebandError {
		t.Fatalf("channel = %d, want SidebandError", channel)
	}
	if string(payload) != "disk full" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestSidebandEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	if err := sw.WriteData(nil); err != nil {
		t.Fatalf("WriteData(nil): %v", err)
	}
	sr := NewSidebandReader(&buf)
	ch, payload, err := sr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if ch != SidebandData || len(payload) != 0 {
		t.Fatalf("unexpected frame: channel=%d, len=%d", ch, len(payload))
	}
}

func TestSidebandDataReader(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	_ = sw.WriteData([]byte("hello"))
	_ = sw.WriteProgress("working...")
	_ = sw.WriteData([]byte(" world"))

	dr := NewSidebandDataReader(&buf, nil)
	all, err := io.ReadAll(dr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(all) != "hello world" {
		t.Fatalf("data = %q, want %q", all, "hello world")
	}
}

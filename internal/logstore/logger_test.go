package logstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pranav-k/forkrisk/internal/logstore"
)

func TestLoggerWriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test_events.jsonl")

	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	entry1 := logstore.LogEntry{
		Seq:       1,
		SimTime:   0.5,
		EventType: "BlockPropose",
		NodeID:    "N0",
		BlockHash: "hash1",
	}
	entry2 := logstore.LogEntry{
		Seq:       2,
		SimTime:   0.6,
		EventType: "BlockGossip",
		NodeID:    "N1",
		FromNode:  "N0",
		BlockHash: "hash1",
	}

	if err := logger.Write(entry1); err != nil {
		t.Fatalf("Write entry1: %v", err)
	}
	if err := logger.Write(entry2); err != nil {
		t.Fatalf("Write entry2: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var parsed []logstore.LogEntry
	lines := splitLines(data)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for _, l := range lines {
		var e logstore.LogEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		parsed = append(parsed, e)
	}

	if parsed[1].FromNode != "N0" {
		t.Errorf("expected FromNode N0, got %s", parsed[1].FromNode)
	}
}

func TestWriteMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	meta := logstore.RunMetadata{
		RunID:          "run123",
		ScenarioName:   "baseline",
		Seed:           42,
		ProposedBlocks: 10,
		LogSHA256:      "abcdef",
	}

	if err := logstore.WriteMetadata(tmpDir, meta); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	metaFile := filepath.Join(tmpDir, "metadata.json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var readMeta logstore.RunMetadata
	if err := json.Unmarshal(data, &readMeta); err != nil {
		t.Fatalf("Unmarshal metadata failed: %v", err)
	}

	if readMeta.RunID != "run123" || readMeta.Seed != 42 || readMeta.ProposedBlocks != 10 {
		t.Errorf("Metadata mismatch: %+v", readMeta)
	}
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := string(data[start:i])
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := string(data[start:])
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

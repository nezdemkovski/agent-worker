package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventLogFileWritesSnapshotAndJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-result.json")
	logFile, err := newEventLogFile(path, nil)
	if err != nil {
		t.Fatalf("newEventLogFile(): %v", err)
	}

	first := NewEvent(CodeServiceStart, LevelInfo, "starting service")
	second := NewEvent(CodeServiceReady, LevelInfo, "service ready")
	if err := logFile.Append(first); err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	if err := logFile.Append(second); err != nil {
		t.Fatalf("Append(second): %v", err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	snapshotData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(snapshot): %v", err)
	}
	var snapshot EventLog
	if err := json.Unmarshal(snapshotData, &snapshot); err != nil {
		t.Fatalf("Unmarshal(snapshot): %v", err)
	}
	if len(snapshot.Events) != 2 {
		t.Fatalf("expected 2 snapshot events, got %d", len(snapshot.Events))
	}

	jsonlData, err := os.ReadFile(path + ".jsonl")
	if err != nil {
		t.Fatalf("ReadFile(jsonl): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(jsonlData)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines, got %d", len(lines))
	}
}

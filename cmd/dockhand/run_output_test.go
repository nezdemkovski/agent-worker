package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func TestCollectArtifactFilesSortsRecursively(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "control"), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "service-result.json"),
		filepath.Join(root, "control", "b.response"),
		filepath.Join(root, "control", "a.response"),
	} {
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	paths, err := collectArtifactFiles(root)
	if err != nil {
		t.Fatalf("collectArtifactFiles(): %v", err)
	}
	var rels []string
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("filepath.Rel(): %v", err)
		}
		rels = append(rels, rel)
	}

	got := strings.Join(rels, ",")
	want := "control/a.response,control/b.response,service-result.json"
	if got != want {
		t.Fatalf("unexpected artifact order: got %q want %q", got, want)
	}
}

func TestEmitRunPreludeIncludesRunMetadata(t *testing.T) {
	output := captureStdout(t, func() {
		emitRunPrelude(&worker.WorkerPayload{
			RunID:     "run-123",
			SessionID: "session-456",
			Mode:      "service",
		}, "/tmp/run.json", "/workspace", "/artifacts")
	})

	for _, expected := range []string{
		"RUN_START run-123",
		"Starting run run-123 in service mode",
		"Session: session-456",
		"Payload file: /tmp/run.json",
		"Workspace dir: /workspace",
		"Artifacts dir: /artifacts",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in output %q", expected, output)
		}
	}
}

func TestEmitArtifactFilesWrapsContentsInMarkers(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "service-result.json")
	if err := os.WriteFile(path, []byte("{\"status\":\"ok\"}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	output := captureStdout(t, func() {
		emitArtifactFiles(root)
	})

	if !strings.Contains(output, "ARTIFACT_FILE_BEGIN service-result.json") {
		t.Fatalf("missing begin marker in %q", output)
	}
	if !strings.Contains(output, "{\"status\":\"ok\"}") {
		t.Fatalf("missing artifact content in %q", output)
	}
	if !strings.Contains(output, "ARTIFACT_FILE_END service-result.json") {
		t.Fatalf("missing end marker in %q", output)
	}
}

package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverMirrordTargetDeletesPodsAndWaitsForRollout(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logFile := filepath.Join(tmpDir, "kubectl.log")
	writeScript(t, filepath.Join(binDir, "kubectl"), "#!/bin/sh\necho \"$@\" >> \"$NDEV_TEST_KUBECTL_LOG\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NDEV_TEST_KUBECTL_LOG", logFile)

	eventFile, err := newEventLogFile(filepath.Join(tmpDir, "service-result.json"), nil)
	if err != nil {
		t.Fatalf("newEventLogFile: %v", err)
	}

	ok := recoverMirrordTarget(context.Background(), "agent-session-test", "noona-api", "deploy/noona-api", filepath.Join(tmpDir, "service.log"), eventFile)
	if !ok {
		t.Fatal("expected recoverMirrordTarget to succeed")
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read kubectl log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "-n agent-session-test delete pod -l app=noona-api --wait=false") {
		t.Fatalf("expected delete pod command, got %q", content)
	}
	if !strings.Contains(content, "-n agent-session-test rollout status deployment/noona-api --timeout=120s") {
		t.Fatalf("expected rollout status command, got %q", content)
	}
}

func TestIsDirtyIptablesLog(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(logFile, []byte("Detected dirty iptables\n"), 0o644); err != nil {
		t.Fatalf("write service log: %v", err)
	}
	if !isDirtyIptablesLog(logFile) {
		t.Fatal("expected dirty iptables marker to be detected")
	}
}

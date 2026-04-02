package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWorkerEnvironmentWritesNetrcAndPropagatesNPMToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NDEV_GIT_TOKEN", "test-token")
	t.Setenv("NDEV_GIT_USERNAME", "octocat")
	t.Setenv("GH_NPM_TOKEN", "")

	if err := prepareWorkerEnvironment(); err != nil {
		t.Fatalf("prepareWorkerEnvironment: %v", err)
	}

	netrcPath := filepath.Join(home, ".netrc")
	data, err := os.ReadFile(netrcPath)
	if err != nil {
		t.Fatalf("read .netrc: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "machine github.com") {
		t.Fatalf("expected github.com machine entry, got %q", content)
	}
	if !strings.Contains(content, "login octocat") {
		t.Fatalf("expected username in .netrc, got %q", content)
	}
	if !strings.Contains(content, "password test-token") {
		t.Fatalf("expected token in .netrc, got %q", content)
	}
	if got := os.Getenv("GH_NPM_TOKEN"); got != "test-token" {
		t.Fatalf("expected GH_NPM_TOKEN propagated, got %q", got)
	}
}

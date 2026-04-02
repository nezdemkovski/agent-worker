package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapRepoServiceModeCloneAndCheckout(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "workspace", "repo1")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
if [ "$1" = "clone" ]; then
  for arg in "$@"; do dest="$arg"; done
  mkdir -p "$dest/.git"
  exit 0
fi
if [ "$1" = "-C" ] && [ "$3" = "checkout" ]; then
  exit 0
fi
if [ "$1" = "-C" ] && [ "$3" = "fetch" ]; then
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := BootstrapRepo(BootstrapRepoOptions{
		Repo:      "repo1",
		RepoDir:   repoDir,
		RemoteURL: "https://example.invalid/repo1.git",
		Branch:    "agent/test-branch",
		RunMode:   RunModeService,
	})
	if err != nil {
		t.Fatalf("BootstrapRepo() error = %v", err)
	}

	if len(result.ClonePlan) != 2 {
		t.Fatalf("expected clone plan lines, got %+v", result.ClonePlan)
	}
	if !strings.Contains(result.ClonePlan[0], "git clone --depth 1") {
		t.Fatalf("expected shallow clone plan, got %q", result.ClonePlan[0])
	}
	if !containsEvent(result.CheckoutEvents, CodeRepoClone, LevelInfo, "Dobby is honored to clone the repository, sir") {
		t.Fatalf("expected clone event, got %+v", result.CheckoutEvents)
	}
	if !containsEvent(result.BranchEvents, CodeRepoBranch, LevelInfo, "Dobby switches to branch agent/test-branch, sir") {
		t.Fatalf("expected branch event, got %+v", result.BranchEvents)
	}
	if result.BranchReady != "repo1:agent/test-branch" {
		t.Fatalf("unexpected branch ready %q", result.BranchReady)
	}
}

func TestBootstrapRepoVerifyModeRunsDependencyBootstrap(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "workspace", "repo1")
	pnpmStore := filepath.Join(root, "pnpm", "store")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"name":"repo1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pnpmStore, "v3", "projects", "stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
exit 0
`)
	writeScript(t, filepath.Join(binDir, "go"), `#!/bin/sh
if [ "$1" = "mod" ] && [ "$2" = "download" ]; then
  echo go-download
  exit 0
fi
exit 0
`)
	writeScript(t, filepath.Join(binDir, "pnpm"), `#!/bin/sh
echo "$@" >> "$NDEV_TEST_PNPM_LOG"
exit 0
`)
	pnpmLog := filepath.Join(root, "pnpm.log")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NDEV_TEST_PNPM_LOG", pnpmLog)

	result, err := BootstrapRepo(BootstrapRepoOptions{
		Repo:         "repo1",
		RepoDir:      repoDir,
		RemoteURL:    "https://example.invalid/repo1.git",
		Branch:       "agent/test-branch",
		RunMode:      RunModeVerify,
		PNPMStoreDir: pnpmStore,
		PNPMStateDir: filepath.Join(root, "pnpm", "state"),
	})
	if err != nil {
		t.Fatalf("BootstrapRepo() error = %v", err)
	}

	if !containsEvent(result.BootstrapEvents, CodeRepoBootstrap, LevelInfo, "Dobby fetches the Go modules. Dobby is a good elf!") {
		t.Fatalf("expected go bootstrap event, got %+v", result.BootstrapEvents)
	}
	if !containsEvent(result.BootstrapEvents, CodeRepoBootstrap, LevelInfo, "Dobby fetches pnpm dependencies, sir. Dobby does not complain") {
		t.Fatalf("expected pnpm fetch event, got %+v", result.BootstrapEvents)
	}
	if dirEntries, _ := os.ReadDir(filepath.Join(pnpmStore, "v3", "projects")); len(dirEntries) != 0 {
		t.Fatalf("expected stale project links to be removed, got %d entries", len(dirEntries))
	}
	logData, err := os.ReadFile(pnpmLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "fetch") {
		t.Fatalf("expected pnpm fetch call, got %q", string(logData))
	}
}

func TestBootstrapRepoSmokeModeSkipsDependencyBootstrap(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "workspace", "repo1")
	pnpmStore := filepath.Join(root, "pnpm", "store")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"name":"repo1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pnpmStore, "v3", "projects", "stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
exit 0
`)
	writeScript(t, filepath.Join(binDir, "go"), `#!/bin/sh
echo unexpected-go-call >&2
exit 99
`)
	writeScript(t, filepath.Join(binDir, "pnpm"), `#!/bin/sh
echo unexpected-pnpm-call >&2
exit 99
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := BootstrapRepo(BootstrapRepoOptions{
		Repo:         "repo1",
		RepoDir:      repoDir,
		RemoteURL:    "https://example.invalid/repo1.git",
		Branch:       "agent/test-branch",
		RunMode:      RunModeSmoke,
		PNPMStoreDir: pnpmStore,
		PNPMStateDir: filepath.Join(root, "pnpm", "state"),
	})
	if err != nil {
		t.Fatalf("BootstrapRepo() error = %v", err)
	}

	if !strings.Contains(result.ClonePlan[0], "git clone --depth 1") {
		t.Fatalf("expected shallow clone plan for smoke mode, got %q", result.ClonePlan[0])
	}
	if !containsEvent(result.BootstrapEvents, CodeRepoBootstrap, LevelWarn, "smoke verification skips go module bootstrap") {
		t.Fatalf("expected smoke go bootstrap skip event, got %+v", result.BootstrapEvents)
	}
	if !containsEvent(result.BootstrapEvents, CodeRepoBootstrap, LevelWarn, "smoke verification skips pnpm bootstrap") {
		t.Fatalf("expected smoke pnpm bootstrap skip event, got %+v", result.BootstrapEvents)
	}
	if dirEntries, _ := os.ReadDir(filepath.Join(pnpmStore, "v3", "projects")); len(dirEntries) != 0 {
		t.Fatalf("expected stale project links to be removed, got %d entries", len(dirEntries))
	}
}

func TestBootstrapRepoCloneFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	repoDir := filepath.Join(root, "workspace", "repo1")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
if [ "$1" = "clone" ]; then
  exit 7
fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := BootstrapRepo(BootstrapRepoOptions{
		Repo:      "repo1",
		RepoDir:   repoDir,
		RemoteURL: "https://example.invalid/repo1.git",
		Branch:    "agent/test-branch",
		RunMode:   RunModeVerify,
	})
	if err == nil {
		t.Fatal("expected fatal clone error")
	}
	if !containsEvent(result.CheckoutEvents, CodeRepoFail, LevelError, "git clone failed") {
		t.Fatalf("expected clone failure event, got %+v", result.CheckoutEvents)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsEvent(events []Event, code EventCode, level EventLevel, message string) bool {
	for _, event := range events {
		if event.Code == code && event.Level == level && event.Message == message {
			return true
		}
	}
	return false
}

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
	if !containsLine(result.CheckoutResult, "CLONE repo1") {
		t.Fatalf("expected clone marker, got %+v", result.CheckoutResult)
	}
	if !containsLine(result.BranchResult, "CHECKOUT repo1 agent/test-branch") {
		t.Fatalf("expected branch marker, got %+v", result.BranchResult)
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

	if !containsLine(result.BootstrapResult, "GO_MOD_DOWNLOAD repo1") {
		t.Fatalf("expected go bootstrap marker, got %+v", result.BootstrapResult)
	}
	if !containsLine(result.BootstrapResult, "PNPM_FETCH repo1") {
		t.Fatalf("expected pnpm fetch marker, got %+v", result.BootstrapResult)
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
	if !containsLine(result.BootstrapResult, "SKIP repo1: smoke verification skips go module bootstrap") {
		t.Fatalf("expected smoke go bootstrap skip marker, got %+v", result.BootstrapResult)
	}
	if !containsLine(result.BootstrapResult, "SKIP repo1: smoke verification skips pnpm bootstrap") {
		t.Fatalf("expected smoke pnpm bootstrap skip marker, got %+v", result.BootstrapResult)
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
	if !containsLine(result.CheckoutResult, "FAIL repo1: git clone") {
		t.Fatalf("expected clone failure marker, got %+v", result.CheckoutResult)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

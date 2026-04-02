package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanStartGoHTTPDefaultsToGoRun(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	entrypoint := filepath.Join(repoDir, "cmd", "svc", "main.go")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entrypoint, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanStart(StartPlanOptions{
		Service:        "svc",
		WorkDir:        repoDir,
		RuntimeProfile: ProfileGoHTTP,
		EntryPoint:     "./cmd/svc/main.go",
		Port:           "31140",
	})
	if err != nil {
		t.Fatalf("PlanStart() error = %v", err)
	}
	if plan.ResolvedStrategy != "go-run" {
		t.Fatalf("expected go-run strategy, got %q", plan.ResolvedStrategy)
	}
	if !strings.Contains(plan.StartCommand, "exec go run './cmd/svc/main.go' --port '31140'") {
		t.Fatalf("unexpected start command: %s", plan.StartCommand)
	}
	if !strings.Contains(plan.StartDescription, "go run ./cmd/svc/main.go --port 31140") {
		t.Fatalf("unexpected start description: %s", plan.StartDescription)
	}
}

func TestPlanStartNodeHTTPNpmAutoBuildFallback(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"build":"tsc","start":"node dist/index.js","dev":"tsx src/index.ts"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanStart(StartPlanOptions{
		WorkDir:        repoDir,
		RuntimeProfile: ProfileNodeHTTP,
		Port:           "3200",
	})
	if err != nil {
		t.Fatalf("PlanStart() error = %v", err)
	}
	if plan.ResolvedStrategy != "npm-auto" {
		t.Fatalf("expected npm-auto strategy, got %q", plan.ResolvedStrategy)
	}
	if !strings.Contains(plan.StartCommand, "if npm run build; then exec npm run start; else") {
		t.Fatalf("unexpected npm-auto command: %s", plan.StartCommand)
	}
	if !strings.Contains(plan.StartDescription, "fallback to npm run dev") {
		t.Fatalf("unexpected description: %s", plan.StartDescription)
	}
}

func TestPlanStartNodeHTTPRequiresStartOrDev(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"build":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := PlanStart(StartPlanOptions{
		WorkDir:        repoDir,
		RuntimeProfile: ProfileNodeHTTP,
		Port:           "3200",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing both start and dev") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanStartRejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	entrypoint := filepath.Join(repoDir, "cmd", "svc", "main.go")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entrypoint, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := PlanStart(StartPlanOptions{
		Service:        "svc",
		WorkDir:        repoDir,
		RuntimeProfile: ProfileGoHTTP,
		EntryPoint:     "./cmd/svc/main.go",
		StartStrategy:  "weird",
		Port:           "31140",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported runtime/start strategy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

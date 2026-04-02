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
	if plan.Strategy != string(StrategyGoRun) {
		t.Fatalf("expected go-run strategy, got %q", plan.Strategy)
	}
	if !strings.Contains(plan.Description, "run go service entrypoint") {
		t.Fatalf("unexpected start description: %s", plan.Description)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Type != StepRun {
		t.Fatalf("expected single run step, got %+v", plan.Steps)
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
	if plan.Strategy != string(StrategyNpmAuto) {
		t.Fatalf("expected npm-auto strategy, got %q", plan.Strategy)
	}
	if !strings.Contains(plan.Description, "fallback to npm run dev") {
		t.Fatalf("unexpected description: %s", plan.Description)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Type != StepRun || plan.Steps[1].Type != StepRun {
		t.Fatalf("expected build/start run steps, got %+v", plan.Steps)
	}
	if len(plan.Fallback) != 1 || plan.Fallback[0].Type != StepRun {
		t.Fatalf("expected single run fallback, got %+v", plan.Fallback)
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

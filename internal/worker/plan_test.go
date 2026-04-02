package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTypedPlanGoRunShape(t *testing.T) {
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
	if plan.Plan == nil {
		t.Fatal("expected typed plan, got nil")
	}

	tp := plan.Plan
	if tp.RuntimeProfile != "go-http" {
		t.Fatalf("expected go-http, got %q", tp.RuntimeProfile)
	}
	if tp.Strategy != "go-run" {
		t.Fatalf("expected go-run, got %q", tp.Strategy)
	}
	if tp.Workdir != repoDir {
		t.Fatalf("expected workdir %s, got %s", repoDir, tp.Workdir)
	}

	// checks
	if len(tp.Checks) < 2 {
		t.Fatalf("expected at least 2 checks, got %d", len(tp.Checks))
	}
	if tp.Checks[0].Type != "file_exists" {
		t.Fatalf("expected file_exists check, got %q", tp.Checks[0].Type)
	}
	if tp.Checks[1].Type != "command_exists" || tp.Checks[1].Name != "go" {
		t.Fatalf("expected command_exists go check, got %+v", tp.Checks[1])
	}

	// steps
	if len(tp.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(tp.Steps))
	}
	step := tp.Steps[0]
	if step.Command != "go" {
		t.Fatalf("expected go command, got %q", step.Command)
	}
	if !step.Exec {
		t.Fatal("expected exec=true on final step")
	}
	expectedArgs := []string{"run", "./cmd/svc/main.go", "--port", "31140"}
	if len(step.Args) != len(expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, step.Args)
	}
	for i, a := range expectedArgs {
		if step.Args[i] != a {
			t.Fatalf("arg[%d]: expected %q, got %q", i, a, step.Args[i])
		}
	}

	// no fallback for go-run
	if len(tp.Fallback) != 0 {
		t.Fatalf("expected no fallback, got %d steps", len(tp.Fallback))
	}
}

func TestTypedPlanNpmAutoWithFallback(t *testing.T) {
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
	if plan.Plan == nil {
		t.Fatal("expected typed plan, got nil")
	}

	tp := plan.Plan
	if tp.Strategy != "npm-auto" {
		t.Fatalf("expected npm-auto, got %q", tp.Strategy)
	}
	if tp.Env["PORT"] != "3200" {
		t.Fatalf("expected PORT=3200, got %q", tp.Env["PORT"])
	}

	// should have build + start steps
	if len(tp.Steps) != 2 {
		t.Fatalf("expected 2 steps (build+start), got %d", len(tp.Steps))
	}
	if tp.Steps[0].Command != "npm" || tp.Steps[0].Args[1] != "build" {
		t.Fatalf("expected npm run build, got %+v", tp.Steps[0])
	}
	if tp.Steps[1].Command != "npm" || tp.Steps[1].Args[1] != "start" {
		t.Fatalf("expected npm run start, got %+v", tp.Steps[1])
	}
	if !tp.Steps[1].Exec {
		t.Fatal("expected exec=true on last step")
	}

	// should have dev fallback
	if len(tp.Fallback) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(tp.Fallback))
	}
	if tp.Fallback[0].Args[1] != "dev" {
		t.Fatalf("expected fallback to npm run dev, got %+v", tp.Fallback[0])
	}
}

func TestTypedPlanNpmAutoStartOnly(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"start":"node index.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanStart(StartPlanOptions{
		WorkDir:        repoDir,
		RuntimeProfile: ProfileNodeHTTP,
		Port:           "3000",
	})
	if err != nil {
		t.Fatalf("PlanStart() error = %v", err)
	}
	tp := plan.Plan
	if tp == nil {
		t.Fatal("expected typed plan")
	}
	if len(tp.Steps) != 1 || tp.Steps[0].Args[1] != "start" {
		t.Fatalf("expected single start step, got %+v", tp.Steps)
	}
	if len(tp.Fallback) != 0 {
		t.Fatalf("expected no fallback, got %d", len(tp.Fallback))
	}
}

func TestTypedPlanPnpmDev(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"dev":"tsx src/index.ts"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanStart(StartPlanOptions{
		WorkDir:        repoDir,
		RuntimeProfile: ProfileNodeHTTP,
		StartStrategy:  "pnpm-dev",
		Port:           "3100",
	})
	if err != nil {
		t.Fatalf("PlanStart() error = %v", err)
	}
	tp := plan.Plan
	if tp == nil {
		t.Fatal("expected typed plan")
	}
	if tp.Strategy != "pnpm-dev" {
		t.Fatalf("expected pnpm-dev, got %q", tp.Strategy)
	}
	// pnpm-dev checks for tsx binary, not node_modules dir
	hasTsxCheck := false
	for _, c := range tp.Checks {
		if c.Type == "file_exists" && filepath.Base(c.Path) == "tsx" {
			hasTsxCheck = true
		}
	}
	if !hasTsxCheck {
		t.Fatal("expected file_exists check for tsx binary")
	}
}

func TestTypedPlanPnpmStart(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"start":"node index.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanStart(StartPlanOptions{
		WorkDir:        repoDir,
		RuntimeProfile: ProfileNodeHTTP,
		StartStrategy:  "pnpm-start",
		Port:           "3100",
	})
	if err != nil {
		t.Fatalf("PlanStart() error = %v", err)
	}
	tp := plan.Plan
	if tp == nil {
		t.Fatal("expected typed plan")
	}
	if tp.Strategy != "pnpm-start" {
		t.Fatalf("expected pnpm-start, got %q", tp.Strategy)
	}
	if len(tp.Steps) != 1 || tp.Steps[0].Args[1] != "start" {
		t.Fatalf("expected single start step, got %+v", tp.Steps)
	}
}

func TestTypedPlanIsValidJSON(t *testing.T) {
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

	data, err := json.Marshal(plan.Plan)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var roundtrip TypedStartPlan
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundtrip.Strategy != plan.Plan.Strategy {
		t.Fatalf("roundtrip strategy mismatch: %q vs %q", roundtrip.Strategy, plan.Plan.Strategy)
	}
	if len(roundtrip.Steps) != len(plan.Plan.Steps) {
		t.Fatalf("roundtrip steps mismatch: %d vs %d", len(roundtrip.Steps), len(plan.Plan.Steps))
	}
}

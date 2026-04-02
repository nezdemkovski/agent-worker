package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunChecksFileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "file_exists", Path: f},
		},
	}
	if err := RunChecks(plan); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestRunChecksFileExistsFails(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "file_exists", Path: "/nonexistent/path/file.txt"},
		},
	}
	err := RunChecks(plan)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRunChecksDirExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "dir_exists", Path: dir},
		},
	}
	if err := RunChecks(plan); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestRunChecksDirExistsFails(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "dir_exists", Path: "/nonexistent/directory"},
		},
	}
	err := RunChecks(plan)
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestRunChecksCommandExists(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "command_exists", Name: "sh"},
		},
	}
	if err := RunChecks(plan); err != nil {
		t.Fatalf("expected pass for sh, got: %v", err)
	}
}

func TestRunChecksCommandExistsFails(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "command_exists", Name: "nonexistent_command_xyz"},
		},
	}
	err := RunChecks(plan)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestRunChecksUnknownType(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "magic_check"},
		},
	}
	err := RunChecks(plan)
	if err == nil {
		t.Fatal("expected error for unknown check type")
	}
}

func TestRunChecksMultiple(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "dir_exists", Path: dir},
			{Type: "file_exists", Path: f},
			{Type: "command_exists", Name: "sh"},
		},
	}
	if err := RunChecks(plan); err != nil {
		t.Fatalf("expected all checks to pass, got: %v", err)
	}
}

func TestRunChecksStopsAtFirstFailure(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "command_exists", Name: "sh"},
			{Type: "file_exists", Path: "/nonexistent"},
			{Type: "command_exists", Name: "sh"}, // should not reach this
		},
	}
	err := RunChecks(plan)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildCommandSetsEnv(t *testing.T) {
	t.Parallel()
	step := PlanStep{
		Command: "sh",
		Args:    []string{"-c", "echo ok"},
		Env:     map[string]string{"STEP_VAR": "step_val"},
	}
	planEnv := map[string]string{"PLAN_VAR": "plan_val"}

	_, _, env, err := BuildCommand(step, planEnv)
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}

	hasStep := false
	hasPlan := false
	for _, e := range env {
		if e == "STEP_VAR=step_val" {
			hasStep = true
		}
		if e == "PLAN_VAR=plan_val" {
			hasPlan = true
		}
	}
	if !hasStep {
		t.Fatal("expected STEP_VAR in env")
	}
	if !hasPlan {
		t.Fatal("expected PLAN_VAR in env")
	}
}

func TestBuildCommandStepEnvOverridesPlanEnv(t *testing.T) {
	t.Parallel()
	step := PlanStep{
		Command: "sh",
		Env:     map[string]string{"PORT": "4000"},
	}
	planEnv := map[string]string{"PORT": "3000"}

	_, _, env, err := BuildCommand(step, planEnv)
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}

	for _, e := range env {
		if e == "PORT=3000" {
			t.Fatal("step env should override plan env, but found PORT=3000")
		}
	}
}

func TestSetEnvVar(t *testing.T) {
	t.Parallel()
	env := []string{"A=1", "B=2"}

	// update existing
	env = setEnvVar(env, "A", "10")
	found := false
	for _, e := range env {
		if e == "A=10" {
			found = true
		}
		if e == "A=1" {
			t.Fatal("old value should be replaced")
		}
	}
	if !found {
		t.Fatal("expected A=10")
	}

	// add new
	env = setEnvVar(env, "C", "3")
	found = false
	for _, e := range env {
		if e == "C=3" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected C=3")
	}
}

func TestParsePlanJSON(t *testing.T) {
	t.Parallel()
	data := []byte(`{
		"runtime_profile": "go-http",
		"strategy": "go-run",
		"workdir": "/tmp/test",
		"checks": [{"type": "command_exists", "name": "go"}],
		"steps": [{"command": "go", "args": ["run", "main.go"], "exec": true}],
		"description": "go run main.go"
	}`)

	plan, err := ParsePlanJSON(data)
	if err != nil {
		t.Fatalf("ParsePlanJSON error: %v", err)
	}
	if plan.Strategy != "go-run" {
		t.Fatalf("expected go-run, got %q", plan.Strategy)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if !plan.Steps[0].Exec {
		t.Fatal("expected exec=true")
	}
}

func TestFormatPlanJSONRoundtrip(t *testing.T) {
	t.Parallel()
	original := &TypedStartPlan{
		RuntimeProfile: "node-http",
		Strategy:       "npm-auto",
		Workdir:        "/tmp/test",
		Env:            map[string]string{"PORT": "3000"},
		Checks:         []PlanCheck{{Type: "dir_exists", Path: "/tmp/test"}},
		Steps: []PlanStep{
			{Command: "npm", Args: []string{"run", "build"}},
			{Command: "npm", Args: []string{"run", "start"}, Exec: true},
		},
		Fallback: []PlanStep{
			{Command: "npm", Args: []string{"run", "dev"}, Exec: true},
		},
		Description: "npm run build && npm run start",
	}

	jsonStr, err := FormatPlanJSON(original)
	if err != nil {
		t.Fatalf("FormatPlanJSON error: %v", err)
	}

	roundtrip, err := ParsePlanJSON([]byte(jsonStr))
	if err != nil {
		t.Fatalf("ParsePlanJSON error: %v", err)
	}

	if roundtrip.Strategy != original.Strategy {
		t.Fatalf("strategy mismatch: %q vs %q", roundtrip.Strategy, original.Strategy)
	}
	if len(roundtrip.Steps) != len(original.Steps) {
		t.Fatalf("steps count mismatch: %d vs %d", len(roundtrip.Steps), len(original.Steps))
	}
	if len(roundtrip.Fallback) != len(original.Fallback) {
		t.Fatalf("fallback count mismatch: %d vs %d", len(roundtrip.Fallback), len(original.Fallback))
	}
	if roundtrip.Env["PORT"] != "3000" {
		t.Fatalf("env PORT mismatch: %q", roundtrip.Env["PORT"])
	}
}

func TestExecPlanChecksFailure(t *testing.T) {
	t.Parallel()
	plan := &TypedStartPlan{
		Checks: []PlanCheck{
			{Type: "command_exists", Name: "nonexistent_tool_abc"},
		},
		Steps: []PlanStep{
			{Command: "echo", Args: []string{"should not reach"}, Exec: true},
		},
	}

	err := ExecPlan(plan)
	if err == nil {
		t.Fatal("expected error from failing check")
	}
}

func TestExecPlanAppliesMkdirAndWriteFileSteps(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".ndev-air")
	file := filepath.Join(root, ".ndev-air.toml")
	plan := &TypedStartPlan{
		Workdir: root,
		Steps: []PlanStep{
			{Type: StepMkdirAll, Path: dir, Mode: 0o755},
			{Type: StepWriteFile, Path: file, Mode: 0o644, Content: "root = \".\"\n"},
		},
	}

	if err := ExecPlan(plan); err != nil {
		t.Fatalf("ExecPlan() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory at %s", dir)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "root = \".\"\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

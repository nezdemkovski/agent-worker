package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// RunChecks validates all preconditions in a typed plan.
// Returns nil if all checks pass, or an error describing the first failure.
func RunChecks(plan *TypedStartPlan) error {
	for _, c := range plan.Checks {
		if err := runCheck(c); err != nil {
			return err
		}
	}
	return nil
}

func runCheck(c PlanCheck) error {
	switch c.Type {
	case "dir_exists":
		info, err := os.Stat(c.Path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("check dir_exists failed: %s", c.Path)
		}
	case "file_exists":
		info, err := os.Stat(c.Path)
		if err != nil || info.IsDir() {
			return fmt.Errorf("check file_exists failed: %s", c.Path)
		}
	case "command_exists":
		if _, err := exec.LookPath(c.Name); err != nil {
			return fmt.Errorf("check command_exists failed: %s", c.Name)
		}
	default:
		return fmt.Errorf("unknown check type: %s", c.Type)
	}
	return nil
}

// BuildCommand constructs the command path, argv, and env for a plan step.
func BuildCommand(step PlanStep, planEnv map[string]string) (string, []string, []string, error) {
	cmdPath, err := exec.LookPath(step.Command)
	if err != nil {
		return "", nil, nil, fmt.Errorf("command not found: %s", step.Command)
	}

	args := append([]string{step.Command}, step.Args...)

	env := os.Environ()
	for k, v := range planEnv {
		env = setEnvVar(env, k, v)
	}
	for k, v := range step.Env {
		env = setEnvVar(env, k, v)
	}

	return cmdPath, args, env, nil
}

// ExecStep runs a single plan step. If step.Exec is true, it replaces
// the current process via syscall.Exec. Otherwise it runs as a subprocess.
func ExecStep(step PlanStep, planEnv map[string]string) error {
	cmdPath, args, env, err := BuildCommand(step, planEnv)
	if err != nil {
		return err
	}

	workdir := step.Workdir
	if workdir != "" {
		if err := os.Chdir(workdir); err != nil {
			return fmt.Errorf("chdir %s: %w", workdir, err)
		}
	}

	if step.Exec {
		return syscall.Exec(cmdPath, args, env)
	}

	cmd := exec.Command(cmdPath, args[1:]...)
	cmd.Dir = workdir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExecPlan runs a typed start plan: checks → steps (with fallback on failure).
// The last step marked Exec=true replaces the process.
func ExecPlan(plan *TypedStartPlan) error {
	if err := RunChecks(plan); err != nil {
		return fmt.Errorf("precondition failed: %w", err)
	}

	if plan.Workdir != "" {
		if err := os.Chdir(plan.Workdir); err != nil {
			return fmt.Errorf("chdir %s: %w", plan.Workdir, err)
		}
	}

	for i, step := range plan.Steps {
		if step.Exec {
			return ExecStep(step, plan.Env)
		}
		if err := ExecStep(step, plan.Env); err != nil {
			if len(plan.Fallback) > 0 {
				fmt.Fprintf(os.Stderr, "[dockhand] step %d failed (%s %s), switching to fallback\n",
					i, step.Command, strings.Join(step.Args, " "))
				return execFallback(plan)
			}
			return fmt.Errorf("step %d failed: %w", i, err)
		}
	}

	return nil
}

func execFallback(plan *TypedStartPlan) error {
	for _, step := range plan.Fallback {
		if err := ExecStep(step, plan.Env); err != nil {
			return fmt.Errorf("fallback step failed: %w", err)
		}
	}
	return nil
}

func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// FormatPlanJSON returns the plan as a JSON string.
func FormatPlanJSON(plan *TypedStartPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParsePlanJSON parses a typed plan from JSON.
func ParsePlanJSON(data []byte) (*TypedStartPlan, error) {
	var plan TypedStartPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

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
	case CheckDirExists:
		info, err := os.Stat(c.Path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("check %s failed: %s", c.Type, c.Path)
		}
	case CheckFileExists:
		info, err := os.Stat(c.Path)
		if err != nil || info.IsDir() {
			return fmt.Errorf("check %s failed: %s", c.Type, c.Path)
		}
	case CheckCommandExists:
		if _, err := exec.LookPath(c.Name); err != nil {
			return fmt.Errorf("check %s failed: %s", c.Type, c.Name)
		}
	default:
		return fmt.Errorf("unknown check type: %s", c.Type)
	}
	return nil
}

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

func ExecStep(step PlanStep, planEnv map[string]string, defaultWorkdir string) error {
	switch resolveStepType(step) {
	case StepMkdirAll:
		mode := os.FileMode(step.Mode)
		if mode == 0 {
			mode = 0o755
		}
		if err := os.MkdirAll(step.Path, mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", step.Path, err)
		}
		return nil
	case StepWriteFile:
		mode := os.FileMode(step.Mode)
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(step.Path, []byte(step.Content), mode); err != nil {
			return fmt.Errorf("write file %s: %w", step.Path, err)
		}
		return nil
	case StepRun:
		cmdPath, args, env, err := BuildCommand(step, planEnv)
		if err != nil {
			return err
		}

		workdir := step.Workdir
		if workdir == "" {
			workdir = defaultWorkdir
		}

		if step.Exec {
			if workdir != "" {
				if err := os.Chdir(workdir); err != nil {
					return fmt.Errorf("chdir %s: %w", workdir, err)
				}
			}
			return syscall.Exec(cmdPath, args, env)
		}

		cmd := exec.Command(cmdPath, args[1:]...)
		cmd.Dir = workdir
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

func ExecPlan(plan *TypedStartPlan) error {
	if err := RunChecks(plan); err != nil {
		return fmt.Errorf("precondition failed: %w", err)
	}

	for i, step := range plan.Steps {
		if err := ExecStep(step, plan.Env, plan.Workdir); err != nil {
			if len(plan.Fallback) > 0 {
				fmt.Fprintf(os.Stderr, "[dockhand] step %d failed, switching to fallback: %v\n", i, err)
				return execFallback(plan)
			}
			return fmt.Errorf("step %d failed: %w", i, err)
		}
	}

	return nil
}

func execFallback(plan *TypedStartPlan) error {
	for _, step := range plan.Fallback {
		if err := ExecStep(step, plan.Env, plan.Workdir); err != nil {
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

func FormatPlanJSON(plan *TypedStartPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ParsePlanJSON(data []byte) (*TypedStartPlan, error) {
	var plan TypedStartPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func resolveStepType(step PlanStep) StepType {
	if step.Type != "" {
		return step.Type
	}
	if step.Command != "" {
		return StepRun
	}
	return ""
}

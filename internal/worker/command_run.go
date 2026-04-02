package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
)

type CommandRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func RunCommand(ctx context.Context, dir string, env []string, stdin string, name string, args ...string) (*CommandRunResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &CommandRunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	result.ExitCode = -1
	return result, err
}

func runLines(ctx context.Context, dir string, env map[string]string, name string, args ...string) ([]string, error) {
	var envSlice []string
	if len(env) > 0 {
		envSlice = os.Environ()
		for k, v := range env {
			envSlice = setEnvVar(envSlice, k, v)
		}
	}
	result, err := RunCommand(ctx, dir, envSlice, "", name, args...)
	return splitOutputLines(result.Stdout, result.Stderr), err
}

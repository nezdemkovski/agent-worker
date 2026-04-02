package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

type SuperviseOptions struct {
	Command      []string
	ReadyURL     string
	ReadyTimeout time.Duration
	PIDFile      string
	LogFile      string
}

type SuperviseResult struct {
	PID      int
	ReadyURL string
}

type SuperviseError struct {
	Code ReasonCode
	Err  error
}

func (e *SuperviseError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *SuperviseError) Unwrap() error { return e.Err }

func Supervise(ctx context.Context, opts SuperviseOptions) (*SuperviseResult, error) {
	if len(opts.Command) == 0 {
		return nil, errors.New("command is required")
	}
	if opts.ReadyURL == "" {
		return nil, errors.New("ready URL is required")
	}
	if opts.ReadyTimeout <= 0 {
		opts.ReadyTimeout = 30 * time.Second
	}

	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	if runtime.GOOS != goosWindows {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var logFile *os.File
	if opts.LogFile != "" {
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		defer f.Close()
		logFile = f
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if logFile == nil {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	pid := cmd.Process.Pid
	if opts.PIDFile != "" {
		if err := os.WriteFile(opts.PIDFile, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
			_ = terminateProcessTree(pid, 2*time.Second)
			_ = cmd.Wait()
			return nil, fmt.Errorf("write pid file: %w", err)
		}
	}

	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(opts.ReadyTimeout)
	defer timeout.Stop()

	for {
		select {
		case err := <-exitCh:
			exitErr := fmt.Errorf("process %d exited before readiness", pid)
			if err != nil {
				exitErr = fmt.Errorf("process %d exited before readiness: %w", pid, err)
			}
			return nil, &SuperviseError{Code: ReasonExitedBeforeReady, Err: exitErr}
		case <-ticker.C:
			ready, err := isReady(opts.ReadyURL)
			if err == nil && ready {
				return &SuperviseResult{PID: pid, ReadyURL: opts.ReadyURL}, nil
			}
		case <-timeout.C:
			_ = terminateProcessTree(pid, 2*time.Second)
			<-exitCh
			return nil, &SuperviseError{
				Code: ReasonTimeout,
				Err:  fmt.Errorf("timeout waiting for readiness at %s", opts.ReadyURL),
			}
		case <-ctx.Done():
			_ = terminateProcessTree(pid, 2*time.Second)
			<-exitCh
			return nil, ctx.Err()
		}
	}
}

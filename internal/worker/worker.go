package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const goosWindows = "windows"

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
			if err == nil {
				return nil, fmt.Errorf("process %d exited before readiness", pid)
			}
			return nil, fmt.Errorf("process %d exited before readiness: %w", pid, err)
		case <-ticker.C:
			ready, err := isReady(opts.ReadyURL)
			if err == nil && ready {
				return &SuperviseResult{PID: pid, ReadyURL: opts.ReadyURL}, nil
			}
		case <-timeout.C:
			_ = terminateProcessTree(pid, 2*time.Second)
			<-exitCh
			return nil, fmt.Errorf("timeout waiting for readiness at %s", opts.ReadyURL)
		case <-ctx.Done():
			_ = terminateProcessTree(pid, 2*time.Second)
			<-exitCh
			return nil, ctx.Err()
		}
	}
}

func Terminate(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if grace <= 0 {
		grace = 2 * time.Second
	}
	return terminateProcessTree(pid, grace)
}

func terminateProcessTree(pid int, grace time.Duration) error {
	if runtime.GOOS == goosWindows {
		process, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		return process.Signal(os.Kill)
	}

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("getpgid %d: %w", pid, err)
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("sigterm process group %d: %w", pgid, err)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		running, err := processRunning(pid)
		if err == nil && !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("sigkill process group %d: %w", pgid, err)
	}
	return nil
}

func processRunning(pid int) (bool, error) {
	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	if runtime.GOOS == goosWindows {
		return true, nil
	}

	cmd := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprintf("%d", pid))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return true, nil
	}
	status := strings.TrimSpace(stdout.String())
	if status == "" {
		return false, nil
	}
	if strings.HasPrefix(status, "Z") {
		return false, nil
	}
	return true, nil
}

func isReady(url string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400, nil
}

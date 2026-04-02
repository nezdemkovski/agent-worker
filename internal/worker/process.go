package worker

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const goosWindows = "windows"

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

// pidCmdline returns the command line of a process by reading /proc/$pid/cmdline
// on Linux or falling back to ps on other platforms. Returns empty string on any
// error (process gone, permission denied, etc.).
func pidCmdline(pid int) string {
	if pid <= 0 {
		return ""
	}

	// Try /proc first (Linux).
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil && len(data) > 0 {
		// /proc/pid/cmdline uses NUL separators between args.
		return strings.TrimRight(strings.ReplaceAll(string(data), "\x00", " "), " ")
	}

	// Fallback: ps -o args= -p PID (macOS, BSDs).
	cmd := exec.Command("ps", "-o", "args=", "-p", fmt.Sprintf("%d", pid))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

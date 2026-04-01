package worker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSuperviseSucceedsAndWritesArtifacts(t *testing.T) {
	t.Parallel()

	readyURL := helperReadyURL(t)
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "service.pid")
	logFile := filepath.Join(tmpDir, "service.log")

	result, err := Supervise(context.Background(), SuperviseOptions{
		Command:      helperCommand("serve", readyURL),
		ReadyURL:     readyURL,
		ReadyTimeout: 5 * time.Second,
		PIDFile:      pidFile,
		LogFile:      logFile,
	})
	if err != nil {
		t.Fatalf("Supervise() error = %v", err)
	}
	if result.PID <= 0 {
		t.Fatalf("expected positive pid, got %d", result.PID)
	}
	if result.ReadyURL != readyURL {
		t.Fatalf("expected ready URL %q, got %q", readyURL, result.ReadyURL)
	}

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("ReadFile(pidFile): %v", err)
	}
	if strings.TrimSpace(string(pidData)) != strconv.Itoa(result.PID) {
		t.Fatalf("pid file mismatch: got %q want %d", strings.TrimSpace(string(pidData)), result.PID)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile(logFile): %v", err)
	}
	if !strings.Contains(string(logData), "helper-server-ready") {
		t.Fatalf("expected readiness marker in log file, got %q", string(logData))
	}

	if err := Terminate(result.PID, 2*time.Second); err != nil {
		t.Fatalf("Terminate(): %v", err)
	}
	waitForProcessExit(t, result.PID, 3*time.Second)
}

func TestSuperviseFailsWhenProcessExitsBeforeReady(t *testing.T) {
	t.Parallel()

	readyURL := helperReadyURL(t)
	_, err := Supervise(context.Background(), SuperviseOptions{
		Command:      helperCommand("exit-immediately"),
		ReadyURL:     readyURL,
		ReadyTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("Supervise() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exited before readiness") {
		t.Fatalf("expected exited-before-readiness error, got %v", err)
	}
	var supErr *SuperviseError
	if !errors.As(err, &supErr) {
		t.Fatalf("expected *SuperviseError, got %T: %v", err, err)
	}
	if supErr.Code != "exited_before_ready" {
		t.Fatalf("expected Code %q, got %q", "exited_before_ready", supErr.Code)
	}
}

func TestSuperviseTimesOutWhenNeverReady(t *testing.T) {
	t.Parallel()

	readyURL := helperReadyURL(t)
	_, err := Supervise(context.Background(), SuperviseOptions{
		Command:      helperCommand("sleep-forever"),
		ReadyURL:     readyURL,
		ReadyTimeout: 300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Supervise() expected error, got nil")
	}
	var supErr *SuperviseError
	if !errors.As(err, &supErr) {
		t.Fatalf("expected *SuperviseError, got %T: %v", err, err)
	}
	if supErr.Code != "timeout" {
		t.Fatalf("expected Code %q, got %q", "timeout", supErr.Code)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout in error message, got %v", err)
	}
}

func TestTerminateKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group assertions are unix-only")
	}
	t.Parallel()

	tmpDir := t.TempDir()
	childPIDFile := filepath.Join(tmpDir, "child.pid")
	cmd := exec.Command("sh", "-c", fmt.Sprintf("sleep 60 & echo $! > %q; wait", childPIDFile))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start(): %v", err)
	}
	parentPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = Terminate(parentPID, 500*time.Millisecond)
		_ = cmd.Wait()
	})

	childPID := waitForPIDFile(t, childPIDFile, 3*time.Second)
	if childPID <= 0 {
		t.Fatalf("expected child pid, got %d", childPID)
	}

	if err := Terminate(parentPID, 2*time.Second); err != nil {
		t.Fatalf("Terminate(): %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	select {
	case <-waitCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("parent process %d did not exit within timeout", parentPID)
	}
	waitForProcessExit(t, childPID, 3*time.Second)
}

func helperCommand(mode string, extra ...string) []string {
	args := []string{"env", "GO_WANT_HELPER_PROCESS=1", os.Args[0], "-test.run=TestWorkerHelperProcess", "--", mode}
	args = append(args, extra...)
	return args
}

func helperReadyURL(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(): %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close(): %v", err)
	}
	return "http://" + addr + "/healthz"
}

func waitForPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("strconv.Atoi(%q): %v", strings.TrimSpace(string(data)), convErr)
			}
			return pid
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %q", path)
	return 0
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d did not exit within %s", pid, timeout)
}

func TestWorkerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	dash := -1
	for i, arg := range args {
		if arg == "--" {
			dash = i
			break
		}
	}
	if dash == -1 || len(args) <= dash+1 {
		fmt.Fprintln(os.Stderr, "missing helper mode")
		os.Exit(2)
	}

	mode := args[dash+1]
	switch mode {
	case "serve":
		if len(args) <= dash+2 {
			fmt.Fprintln(os.Stderr, "missing ready URL")
			os.Exit(2)
		}
		runServeHelper(args[dash+2])
	case "exit-immediately":
		os.Exit(1)
	case "sleep-forever":
		for {
			time.Sleep(1 * time.Second)
		}
	case "spawn-child":
		fmt.Fprintln(os.Stderr, "spawn-child helper is no longer used")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(2)
	}
}

func runServeHelper(readyURL string) {
	addr := strings.TrimPrefix(strings.TrimSuffix(readyURL, "/healthz"), "http://")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer ln.Close()

	fmt.Println("helper-server-ready")
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
		_ = conn.Close()
	}
}

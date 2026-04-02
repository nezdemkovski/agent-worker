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

func TestMonitorExitsWhenProcessDies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signals are unix-only")
	}
	t.Parallel()

	args := helperCommand("sleep-forever")
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start(): %v", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	done := make(chan error, 1)
	go func() {
		done <- Monitor(MonitorOptions{PID: pid, Interval: 50 * time.Millisecond})
	}()

	// Terminate the process; Monitor should return promptly.
	if err := Terminate(pid, 2*time.Second); err != nil {
		t.Fatalf("Terminate(): %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Monitor() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Monitor() did not return after process was terminated")
	}
}

func TestMonitorReturnsImmediatelyForDeadPID(t *testing.T) {
	t.Parallel()

	// PID 999999999 is virtually guaranteed not to exist.
	err := Monitor(MonitorOptions{PID: 999999999, Interval: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Monitor() error = %v", err)
	}
}

func TestRestartTerminatesOldAndStartsNew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group assertions are unix-only")
	}
	t.Parallel()

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "service.pid")
	logFile := filepath.Join(tmpDir, "service.log")
	newReadyURL := helperReadyURL(t)

	// Start an old process and record its pid
	oldArgs := helperCommand("sleep-forever")
	oldCmd := exec.Command(oldArgs[0], oldArgs[1:]...)
	oldCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := oldCmd.Start(); err != nil {
		t.Fatalf("start old process: %v", err)
	}
	oldPID := oldCmd.Process.Pid
	t.Cleanup(func() {
		_ = oldCmd.Process.Kill()
		_ = oldCmd.Wait()
	})
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", oldPID)), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	// Reap old process once it exits so waitForProcessExit sees ESRCH.
	// Without this the process stays a zombie (Kill(pid,0) succeeds on zombies).
	go func() { _ = oldCmd.Wait() }()

	result, err := Restart(context.Background(), RestartOptions{
		PIDFile:      pidFile,
		Command:      helperCommand("serve", newReadyURL),
		ReadyURL:     newReadyURL,
		ReadyTimeout: 5 * time.Second,
		LogFile:      logFile,
	})
	if err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if result.OldPID != oldPID {
		t.Fatalf("expected OldPID=%d, got %d", oldPID, result.OldPID)
	}
	if result.NewPID <= 0 || result.NewPID == oldPID {
		t.Fatalf("unexpected NewPID=%d (OldPID=%d)", result.NewPID, oldPID)
	}
	if result.ReadyURL != newReadyURL {
		t.Fatalf("expected ReadyURL=%q, got %q", newReadyURL, result.ReadyURL)
	}

	waitForProcessExit(t, oldPID, 3*time.Second)

	if err := Terminate(result.NewPID, 2*time.Second); err != nil {
		t.Logf("terminate new process: %v", err)
	}
}

func TestRestartWhenOldProcessAlreadyDead(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "service.pid")
	newReadyURL := helperReadyURL(t)

	// Write a pid that is guaranteed not to be running
	if err := os.WriteFile(pidFile, []byte("999999999\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	result, err := Restart(context.Background(), RestartOptions{
		PIDFile:      pidFile,
		Command:      helperCommand("serve", newReadyURL),
		ReadyURL:     newReadyURL,
		ReadyTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if result.OldPID != 999999999 {
		t.Fatalf("expected OldPID=999999999, got %d", result.OldPID)
	}
	if result.NewPID <= 0 {
		t.Fatalf("expected positive NewPID, got %d", result.NewPID)
	}

	if err := Terminate(result.NewPID, 2*time.Second); err != nil {
		t.Logf("terminate new process: %v", err)
	}
}

func TestRestartFailsWhenNewProcessExitsBeforeReady(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "service.pid")
	readyURL := helperReadyURL(t)

	_, err := Restart(context.Background(), RestartOptions{
		PIDFile:      pidFile,
		Command:      helperCommand("exit-immediately"),
		ReadyURL:     readyURL,
		ReadyTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("Restart() expected error, got nil")
	}
	var supErr *SuperviseError
	if !errors.As(err, &supErr) {
		t.Fatalf("expected *SuperviseError, got %T: %v", err, err)
	}
	if supErr.Code != "exited_before_ready" {
		t.Fatalf("expected Code %q, got %q", "exited_before_ready", supErr.Code)
	}
}

func TestHashGoProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme\n"), 0o644)

	h := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})
	if h == "" {
		t.Fatal("expected non-empty hash for go-http profile")
	}

	// README.md shouldn't affect hash
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})
	if h != h2 {
		t.Fatal("hash should be deterministic")
	}
}

func TestHashNodeProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte("console.log('hi')\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:\n"), 0o644)

	h := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "node-http"})
	if h == "" {
		t.Fatal("expected non-empty hash for node-http profile")
	}
}

func TestHashDefaultProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "deep.txt"), []byte("deep\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a", "shallow.txt"), []byte("shallow\n"), 0o644)

	h := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ""})
	if h == "" {
		t.Fatal("expected non-empty hash for default profile")
	}
}

func TestHashEmptyDir(t *testing.T) {
	t.Parallel()
	h := Hash(HashOptions{RepoDir: t.TempDir(), RuntimeProfile: "go-http"})
	if h != "" {
		t.Fatalf("expected empty hash for dir with no matching files, got %q", h)
	}
}

func TestHashMissingDir(t *testing.T) {
	t.Parallel()
	h := Hash(HashOptions{RepoDir: "/nonexistent-dir-12345"})
	if h != "" {
		t.Fatalf("expected empty hash for missing dir, got %q", h)
	}
}

func TestHashDeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)

	h1 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q != %q", h1, h2)
	}
}

func TestHashChangesWhenContentChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main\n"), 0o644)
	h1 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})

	os.WriteFile(goFile, []byte("package main\nfunc init() {}\n"), 0o644)
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: "go-http"})

	if h1 == h2 {
		t.Fatal("hash should change when file content changes")
	}
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

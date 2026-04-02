package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	if result.Probe.StatusCode != 200 {
		t.Fatalf("expected probe status 200, got %d", result.Probe.StatusCode)
	}
	if !strings.Contains(result.Probe.Headers, "HTTP/1.1 200 OK") {
		t.Fatalf("expected probe status line in headers, got %q", result.Probe.Headers)
	}
	if !strings.Contains(result.Probe.Body, "ok") {
		t.Fatalf("expected probe body content, got %q", result.Probe.Body)
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
	if supErr.Code != ReasonExitedBeforeReady {
		t.Fatalf("expected Code %q, got %q", ReasonExitedBeforeReady, supErr.Code)
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
	if supErr.Code != ReasonTimeout {
		t.Fatalf("expected Code %q, got %q", ReasonTimeout, supErr.Code)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout in error message, got %v", err)
	}
}

func TestTerminateKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == goosWindows {
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

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for file %q", path)
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
	if runtime.GOOS == goosWindows {
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

func TestFormatEventLogLineIncludesStableMetadata(t *testing.T) {
	event := Event{
		Time:    "2026-04-02T12:00:00Z",
		Level:   LevelInfo,
		Code:    CodeServiceStart,
		Message: "starting service",
		Service: "noona-api",
		Details: map[string]string{
			"target": "deploy/noona-api",
			"url":    "http://127.0.0.1:31140",
		},
	}

	got := formatEventLogLine(event)
	want := "2026-04-02T12:00:00Z [info] service noona-api: starting service (target=deploy/noona-api, url=http://127.0.0.1:31140)"
	if got != want {
		t.Fatalf("unexpected log line:\n got: %q\nwant: %q", got, want)
	}
}

func TestAppendBootstrapTimelineUsesTypedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap-result.json")
	logFile, err := newEventLogFile(path, nil)
	if err != nil {
		t.Fatalf("newEventLogFile(): %v", err)
	}
	defer logFile.Close()

	result := &BootstrapRepoResult{
		BranchEvents: []Event{{
			Time:    "2026-04-02T12:00:00Z",
			Kind:    "repo",
			Level:   LevelInfo,
			Code:    CodeRepoBranch,
			Message: "checking out branch agent/test-branch",
			Repo:    "noona-api",
			Details: map[string]string{"branch": "agent/test-branch"},
		}},
	}

	appendBootstrapTimeline(logFile, "noona-api", "/workspace/noona-api", result)

	if len(logFile.log.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(logFile.log.Events))
	}
	event := logFile.log.Events[1]
	if event.Code != CodeRepoBranch {
		t.Fatalf("expected repo branch code, got %q", event.Code)
	}
	if event.Message != "checking out branch agent/test-branch" {
		t.Fatalf("unexpected message %q", event.Message)
	}
	if event.Repo != "noona-api" {
		t.Fatalf("unexpected repo %q", event.Repo)
	}
	if event.Details["branch"] != "agent/test-branch" {
		t.Fatalf("unexpected branch detail %#v", event.Details)
	}
}

func TestDescribeControlRequest(t *testing.T) {
	req := &ControlRequest{
		Action:  ActionExec,
		Service: "noona-api",
		Payload: json.RawMessage(`{"command":["echo","hello"]}`),
	}

	message, details := describeControlRequest(req)
	if message != "received command execution request" {
		t.Fatalf("unexpected message %q", message)
	}
	if details["command"] != "echo hello" {
		t.Fatalf("unexpected details %#v", details)
	}
}

func TestDescribeControlRequestPromptIncludesPromptPreview(t *testing.T) {
	req := &ControlRequest{
		Action:  ActionPrompt,
		Service: "noona-api",
		Payload: json.RawMessage(`{"tool":"codex","repo":"noona-api","prompt":"fix login flow and keep tests green"}`),
	}

	message, details := describeControlRequest(req)
	if message != "received prompt request" {
		t.Fatalf("unexpected message %q", message)
	}
	if details["prompt"] != "fix login flow and keep tests green" {
		t.Fatalf("unexpected prompt detail %#v", details)
	}
}

func TestExecPlanHonorsExecWorkdir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workdir := filepath.Join(root, "repo")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	outputPath := filepath.Join(root, "cwd.txt")

	cmd := exec.Command("env",
		"GO_WANT_HELPER_PROCESS=1",
		"HELPER_WORKDIR="+workdir,
		"HELPER_OUTPUT="+outputPath,
		os.Args[0],
		"-test.run=TestWorkerHelperProcess",
		"--",
		"exec-plan-cwd",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper exec-plan run failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", outputPath, err)
	}
	got := strings.TrimSpace(string(data))
	want, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workdir, err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	if gotResolved != want {
		t.Fatalf("expected exec step cwd %q, got %q", want, gotResolved)
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

func TestProcessStatusRunning(t *testing.T) {
	t.Parallel()
	// Current process is always running.
	s := ProcessStatus(os.Getpid())
	if s != StateRunning {
		t.Fatalf("expected running for own pid, got %q", s)
	}
}

func TestProcessStatusGone(t *testing.T) {
	t.Parallel()
	s := ProcessStatus(999999999)
	if s != StateGone {
		t.Fatalf("expected gone for dead pid, got %q", s)
	}
}

func TestProcessStatusInvalidPID(t *testing.T) {
	t.Parallel()
	s := ProcessStatus(0)
	if s != StateGone {
		t.Fatalf("expected gone for pid 0, got %q", s)
	}
}

func TestRestartTerminatesOldAndStartsNew(t *testing.T) {
	if runtime.GOOS == goosWindows {
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
	if result.Probe.StatusCode != 200 {
		t.Fatalf("expected probe status 200, got %d", result.Probe.StatusCode)
	}

	waitForProcessExit(t, oldPID, 3*time.Second)

	if err := Terminate(result.NewPID, 2*time.Second); err != nil {
		t.Logf("terminate new process: %v", err)
	}
}

func TestRestartReturnsSourceHashesWhenConfigured(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "service.pid")
	repoDir := filepath.Join(tmpDir, "repo")
	srcDir := filepath.Join(repoDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "index.ts"), []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	readyURL := helperReadyURL(t)
	result, err := Restart(context.Background(), RestartOptions{
		PIDFile:      pidFile,
		Command:      helperCommand("serve", readyURL),
		ReadyURL:     readyURL,
		ReadyTimeout: 5 * time.Second,
		RepoDir:      repoDir,
		Profile:      ProfileNodeHTTP,
	})
	if err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if result.OldSourceHash == "" || result.NewSourceHash == "" {
		t.Fatalf("expected non-empty source hashes, got old=%q new=%q", result.OldSourceHash, result.NewSourceHash)
	}
	if result.OldSourceHash != result.NewSourceHash {
		t.Fatalf("expected unchanged source hash across restart, got old=%q new=%q", result.OldSourceHash, result.NewSourceHash)
	}

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
	if supErr.Code != ReasonExitedBeforeReady {
		t.Fatalf("expected Code %q, got %q", ReasonExitedBeforeReady, supErr.Code)
	}
}

func TestHashGoProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme\n"), 0o644)

	h := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})
	if h == "" {
		t.Fatal("expected non-empty hash for go-http profile")
	}

	// README.md shouldn't affect hash
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})
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

	h := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileNodeHTTP})
	if h == "" {
		t.Fatal("expected non-empty hash for node-http profile")
	}

	expectedFiles := []string{
		filepath.Join(dir, "package.json"),
		filepath.Join(dir, "src", "index.ts"),
	}
	sort.Strings(expectedFiles)
	if h != testExpectedManifestHash(t, dir, expectedFiles) {
		t.Fatalf("unexpected node-http hash: got %q", h)
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
	h := Hash(HashOptions{RepoDir: t.TempDir(), RuntimeProfile: ProfileGoHTTP})
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

func TestRunServiceModeBootstrapsStartsAndHandlesRestart(t *testing.T) {
	t.Parallel()

	readyURL := helperReadyURL(t)
	parsedURL, err := url.Parse(readyURL)
	if err != nil {
		t.Fatalf("url.Parse(): %v", err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("net.SplitHostPort(): %v", err)
	}

	tmpDir := t.TempDir()
	remoteRepo := filepath.Join(tmpDir, "remote-repo")
	createTestGitRepo(t, remoteRepo)

	payload := WorkerPayload{
		RunID:               "run-1",
		Branch:              "agent/test-branch",
		Services:            []string{"noona-api"},
		ServicePlan:         []string{"service plan"},
		VerificationProfile: "smoke",
		VerificationPlan:    []string{"verification plan"},
		Mode:                "service",
		Repos:               []string{"noona-api"},
		RepoSpecs: []RepoSpec{{
			Name: "noona-api",
			URL:  remoteRepo,
			Path: filepath.Join(tmpDir, "workspace", "noona-api"),
		}},
		ServiceSpecs: []ServiceSpec{{
			Name:                    "noona-api",
			ServiceName:             "noona-api",
			Repo:                    "noona-api",
			Workdir:                 filepath.Join(tmpDir, "workspace", "noona-api"),
			RuntimeProfile:          string(ProfileGoHTTP),
			Target:                  "deploy/noona-api",
			Entrypoint:              "./cmd/noona-api/main.go",
			ReadinessPath:           "/healthz",
			StartStrategy:           string(StrategyGoRun),
			DevPort:                 mustAtoi(t, port),
			ReadinessTimeoutSeconds: 5,
		}},
	}
	payloadPath := filepath.Join(tmpDir, "payload.json")
	payloadData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload): %v", err)
	}
	if err := os.WriteFile(payloadPath, payloadData, 0o644); err != nil {
		t.Fatalf("WriteFile(payload): %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- Run(ctx, RunOptions{
			PayloadPath:  payloadPath,
			WorkspaceDir: filepath.Join(tmpDir, "workspace"),
			ArtifactsDir: filepath.Join(tmpDir, "artifacts"),
			PlanCommandBuilder: func(planFile, target string) []string {
				return helperCommand("serve", readyURL)
			},
		})
	}()

	readyPath := filepath.Join(tmpDir, "artifacts", "service-ready.json")
	if err := waitForFileContainsJSONField(readyPath, "status", "ready", 5*time.Second); err != nil {
		t.Fatalf("wait for service-ready.json: %v", err)
	}

	controlDir := filepath.Join(tmpDir, "artifacts", "control")
	requestPath := filepath.Join(controlDir, "restart.request")
	responsePath := filepath.Join(controlDir, "restart.response")
	request := ControlRequest{Version: 1, RequestID: "req-1", Action: ActionRestart, Service: "noona-api"}
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal(request): %v", err)
	}
	if err := os.WriteFile(requestPath, requestData, 0o644); err != nil {
		t.Fatalf("WriteFile(request): %v", err)
	}
	waitForFile(t, responsePath, 5*time.Second)
	responseData, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatalf("ReadFile(response): %v", err)
	}
	var response ControlResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("json.Unmarshal(response): %v", err)
	}
	if response.Status != StatusOK {
		t.Fatalf("expected ok restart response, got %+v", response)
	}

	cancel()
	select {
	case err := <-runErrCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after cancel")
	}
}

func TestRunVerifyModeSmokeBootstrapsAndSucceeds(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	remoteRepo := filepath.Join(tmpDir, "remote-repo")
	createTestGitRepo(t, remoteRepo)

	payload := WorkerPayload{
		RunID:               "run-verify-1",
		Branch:              "agent/test-verify-branch",
		VerificationProfile: "smoke",
		VerificationPlan:    []string{"verification plan"},
		Mode:                "verify",
		Repos:               []string{"noona-api"},
		RepoSpecs: []RepoSpec{{
			Name: "noona-api",
			URL:  remoteRepo,
			Path: filepath.Join(tmpDir, "workspace", "noona-api"),
		}},
	}
	payloadPath := filepath.Join(tmpDir, "payload.json")
	payloadData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload): %v", err)
	}
	if err := os.WriteFile(payloadPath, payloadData, 0o644); err != nil {
		t.Fatalf("WriteFile(payload): %v", err)
	}

	if err := Run(context.Background(), RunOptions{
		PayloadPath:  payloadPath,
		WorkspaceDir: filepath.Join(tmpDir, "workspace"),
		ArtifactsDir: filepath.Join(tmpDir, "artifacts"),
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	workspaceManifest, err := os.ReadFile(filepath.Join(tmpDir, "artifacts", "workspace.txt"))
	if err != nil {
		t.Fatalf("ReadFile(workspace.txt): %v", err)
	}
	if strings.TrimSpace(string(workspaceManifest)) != "noona-api" {
		t.Fatalf("unexpected workspace manifest: %q", string(workspaceManifest))
	}

	var verificationLog EventLog
	verificationData, err := os.ReadFile(filepath.Join(tmpDir, "artifacts", "verification-result.json"))
	if err != nil {
		t.Fatalf("ReadFile(verification-result.json): %v", err)
	}
	if err := json.Unmarshal(verificationData, &verificationLog); err != nil {
		t.Fatalf("json.Unmarshal(verification-result.json): %v", err)
	}
	if !hasEventCode(verificationLog.Events, CodeVerificationSmoke) {
		t.Fatalf("expected verification smoke event, got %+v", verificationLog.Events)
	}
	if !hasEventCode(verificationLog.Events, CodeVerificationOK) {
		t.Fatalf("expected verification ok event, got %+v", verificationLog.Events)
	}

	var mirrordLog EventLog
	mirrordData, err := os.ReadFile(filepath.Join(tmpDir, "artifacts", "mirrord-result.json"))
	if err != nil {
		t.Fatalf("ReadFile(mirrord-result.json): %v", err)
	}
	if err := json.Unmarshal(mirrordData, &mirrordLog); err != nil {
		t.Fatalf("json.Unmarshal(mirrord-result.json): %v", err)
	}
	if !hasEventCode(mirrordLog.Events, CodeMirrordSkip) {
		t.Fatalf("expected mirrord skip event, got %+v", mirrordLog.Events)
	}
}

func TestRunVerifyModeSmokeIgnoresMirrordProbeFailure(t *testing.T) {
	tmpDir := t.TempDir()
	remoteRepo := filepath.Join(tmpDir, "remote-repo")
	createTestGitRepo(t, remoteRepo)
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin): %v", err)
	}
	writeScript(t, filepath.Join(binDir, "mirrord"), `#!/bin/sh
echo no ready pod >&2
exit 23
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	payload := WorkerPayload{
		RunID:               "run-verify-smoke-mirrord",
		Branch:              "agent/test-verify-branch",
		VerificationProfile: "smoke",
		Mode:                "verify",
		Repos:               []string{"noona-api"},
		RepoSpecs: []RepoSpec{{
			Name: "noona-api",
			URL:  remoteRepo,
			Path: filepath.Join(tmpDir, "workspace", "noona-api"),
		}},
		Services: []string{"noona-api"},
		ServiceSpecs: []ServiceSpec{{
			Name:    "noona-api",
			Target:  "deploy/noona-api",
			Workdir: filepath.Join(tmpDir, "workspace", "noona-api"),
		}},
	}
	payloadPath := filepath.Join(tmpDir, "payload.json")
	payloadData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload): %v", err)
	}
	if err := os.WriteFile(payloadPath, payloadData, 0o644); err != nil {
		t.Fatalf("WriteFile(payload): %v", err)
	}

	if err := Run(context.Background(), RunOptions{
		PayloadPath:  payloadPath,
		WorkspaceDir: filepath.Join(tmpDir, "workspace"),
		ArtifactsDir: filepath.Join(tmpDir, "artifacts"),
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var mirrordLog EventLog
	mirrordData, err := os.ReadFile(filepath.Join(tmpDir, "artifacts", "mirrord-result.json"))
	if err != nil {
		t.Fatalf("ReadFile(mirrord-result.json): %v", err)
	}
	if err := json.Unmarshal(mirrordData, &mirrordLog); err != nil {
		t.Fatalf("json.Unmarshal(mirrord-result.json): %v", err)
	}
	if !hasEventCode(mirrordLog.Events, CodeMirrordFail) {
		t.Fatalf("expected mirrord fail event, got %+v", mirrordLog.Events)
	}
}

func createTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "noona-api"), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "noona-api", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go): %v", err)
	}
	cmd := exec.Command("git", "init", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	commit := exec.Command("git", "-C", dir, "add", ".")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}
	commit = exec.Command("git", "-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
}

func hasEventCode(events []Event, code EventCode) bool {
	for _, event := range events {
		if event.Code == code {
			return true
		}
	}
	return false
}

func waitForFileContainsJSONField(path, field, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err == nil {
				if got, ok := payload[field].(string); ok && got == want {
					return nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %q to have %s=%q", path, field, want)
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("strconv.Atoi(%q): %v", value, err)
	}
	return n
}

func TestHashDeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)

	h1 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q != %q", h1, h2)
	}
}

func TestHashChangesWhenContentChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main\n"), 0o644)
	h1 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})

	os.WriteFile(goFile, []byte("package main\nfunc init() {}\n"), 0o644)
	h2 := Hash(HashOptions{RepoDir: dir, RuntimeProfile: ProfileGoHTTP})

	if h1 == h2 {
		t.Fatal("hash should change when file content changes")
	}
}

func testExpectedManifestHash(t *testing.T, repoDir string, files []string) string {
	t.Helper()

	h := sha256.New()
	for _, f := range files {
		rel, err := filepath.Rel(repoDir, f)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q): %v", repoDir, f, err)
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("os.ReadFile(%q): %v", f, err)
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
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
	case "exec-plan-cwd":
		workdir := os.Getenv("HELPER_WORKDIR")
		output := os.Getenv("HELPER_OUTPUT")
		if workdir == "" || output == "" {
			fmt.Fprintln(os.Stderr, "missing HELPER_WORKDIR or HELPER_OUTPUT")
			os.Exit(2)
		}
		plan := &TypedStartPlan{
			Workdir: workdir,
			Steps: []PlanStep{
				{
					Type:    StepRun,
					Command: "sh",
					Args:    []string{"-c", "pwd > \"$1\"", "--", output},
					Workdir: workdir,
					Exec:    true,
				},
			},
		}
		if err := ExecPlan(plan); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
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

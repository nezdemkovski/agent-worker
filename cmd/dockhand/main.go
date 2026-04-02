package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "bootstrap-repo":
		os.Exit(runBootstrapRepo(os.Args[2:]))
	case "run":
		os.Exit(runRun(os.Args[2:]))
	case "exec-plan":
		os.Exit(runExecPlan(os.Args[2:]))
	case "plan-start":
		os.Exit(runPlanStart(os.Args[2:]))
	case "supervise":
		os.Exit(runSupervise(os.Args[2:]))
	case "terminate":
		os.Exit(runTerminate(os.Args[2:]))
	case "restart":
		os.Exit(runRestart(os.Args[2:]))
	case "monitor":
		os.Exit(runMonitor(os.Args[2:]))
	case "hash":
		os.Exit(runHash(os.Args[2:]))
	case "control":
		os.Exit(runControl(os.Args[2:]))
	case "submit-control":
		os.Exit(runSubmitControl(os.Args[2:]))
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: fmt.Sprintf("unknown subcommand %q", os.Args[1]),
		})
		os.Exit(2)
	}
}

func runBootstrapRepo(args []string) int {
	fs := flag.NewFlagSet("bootstrap-repo", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	repo := fs.String("repo", "", "repo name")
	repoDir := fs.String("repo-dir", "", "repo checkout path")
	remoteURL := fs.String("remote-url", "", "remote clone url")
	branch := fs.String("branch", "", "branch to checkout")
	runMode := fs.String("run-mode", "", "run mode")
	pnpmStoreDir := fs.String("pnpm-store-dir", "", "pnpm store dir")
	pnpmStateDir := fs.String("pnpm-state-dir", "", "pnpm state dir")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	result, err := worker.BootstrapRepo(worker.BootstrapRepoOptions{
		Repo:         *repo,
		RepoDir:      *repoDir,
		RemoteURL:    *remoteURL,
		Branch:       *branch,
		RunMode:      worker.RunMode(*runMode),
		PNPMStoreDir: *pnpmStoreDir,
		PNPMStateDir: *pnpmStateDir,
	})
	if err != nil {
		emitJSON(bootstrapRepoResponse{
			Version:         responseVersion,
			Status:          worker.StatusError,
			Reason:          err.Error(),
			ClonePlan:       result.ClonePlan,
			CheckoutResult:  result.CheckoutResult,
			BranchResult:    result.BranchResult,
			BootstrapPlan:   result.BootstrapPlan,
			BootstrapResult: result.BootstrapResult,
			BranchReady:     result.BranchReady,
			PreparedRepo:    result.PreparedRepo,
		})
		return 1
	}

	emitJSON(bootstrapRepoResponse{
		Version:         responseVersion,
		Status:          worker.StatusOK,
		ClonePlan:       result.ClonePlan,
		CheckoutResult:  result.CheckoutResult,
		BranchResult:    result.BranchResult,
		BootstrapPlan:   result.BootstrapPlan,
		BootstrapResult: result.BootstrapResult,
		BranchReady:     result.BranchReady,
		PreparedRepo:    result.PreparedRepo,
	})
	return 0
}

func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	payloadFile := fs.String("payload-file", "", "path to worker payload JSON")
	workspaceDir := fs.String("workspace-dir", "", "workspace root directory")
	artifactsDir := fs.String("artifacts-dir", "", "artifacts directory")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	payload, err := readRunPayload(*payloadFile)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitRunPrelude(payload, *payloadFile, *workspaceDir, *artifactsDir)

	err = worker.Run(context.Background(), worker.RunOptions{
		PayloadPath:         *payloadFile,
		WorkspaceDir:        *workspaceDir,
		ArtifactsDir:        *artifactsDir,
		DefaultServicePort:  os.Getenv("NDEV_SERVICE_PORT"),
		DefaultReadyPath:    os.Getenv("NDEV_SERVICE_READY_PATH"),
		DefaultReadyTimeout: 180 * time.Second,
	})
	emitArtifactFiles(*artifactsDir)
	if err != nil {
		fmt.Printf("RUN_FAIL %s %s\n", payload.RunID, err.Error())
		fmt.Printf("Run %s failed: %s\n", payload.RunID, err.Error())
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}
	fmt.Printf("RUN_DONE %s\n", payload.RunID)
	fmt.Printf("Run %s completed successfully\n", payload.RunID)
	emitJSON(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}{Version: responseVersion, Status: worker.StatusOK})
	return 0
}

func readRunPayload(path string) (*worker.WorkerPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	var payload worker.WorkerPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	return &payload, nil
}

func emitRunPrelude(payload *worker.WorkerPayload, payloadFile, workspaceDir, artifactsDir string) {
	fmt.Printf("RUN_START %s\n", payload.RunID)
	fmt.Printf("Starting run %s in %s mode\n", payload.RunID, payload.Mode)
	fmt.Printf("Session: %s\n", payload.SessionID)
	fmt.Printf("Payload file: %s\n", payloadFile)
	fmt.Printf("Workspace dir: %s\n", workspaceDir)
	fmt.Printf("Artifacts dir: %s\n", artifactsDir)
}

func emitArtifactFiles(root string) {
	paths, err := collectArtifactFiles(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emit artifacts: %v\n", err)
		return
	}
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Printf("ARTIFACT_FILE_BEGIN %s\n", rel)
		if len(data) > 0 {
			fmt.Print(string(data))
			if data[len(data)-1] != '\n' {
				fmt.Println()
			}
		}
		fmt.Printf("ARTIFACT_FILE_END %s\n", rel)
	}
}

func collectArtifactFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func runPlanStart(args []string) int {
	fs := flag.NewFlagSet("plan-start", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	service := fs.String("service", "", "service name")
	workDir := fs.String("workdir", "", "service working directory")
	profile := fs.String("runtime-profile", "", "runtime profile")
	entrypoint := fs.String("entrypoint", "", "service entrypoint")
	strategy := fs.String("start-strategy", "", "requested start strategy")
	port := fs.String("port", "", "service port")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	plan, err := worker.PlanStart(worker.StartPlanOptions{
		Service:        *service,
		WorkDir:        *workDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
		EntryPoint:     *entrypoint,
		StartStrategy:  *strategy,
		Port:           *port,
	})
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitJSON(startPlanResponse{
		Version:          responseVersion,
		Status:           worker.StatusOK,
		RuntimeProfile:   plan.RuntimeProfile,
		StartStrategy:    plan.Strategy,
		StartDescription: plan.Description,
		Plan:             plan,
	})
	return 0
}

func runExecPlan(args []string) int {
	fs := flag.NewFlagSet("exec-plan", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	planFile := fs.String("plan-file", "", "path to plan JSON file (default: read from stdin)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	var planData []byte
	var err error
	if *planFile != "" {
		planData, err = os.ReadFile(*planFile)
	} else {
		planData, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read plan: %v", err)})
		return 1
	}

	plan, err := worker.ParsePlanJSON(planData)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse plan: %v", err)})
		return 1
	}

	// ExecPlan replaces the process on the final exec step.
	// If it returns, either all non-exec steps completed or there was an error.
	if err := worker.ExecPlan(plan); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}
	return 0
}

func runSupervise(args []string) int {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	pidFile := fs.String("pid-file", "", "path to write child pid")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 2
	}

	result, err := worker.Supervise(context.Background(), worker.SuperviseOptions{
		Command:      cmdArgs,
		ReadyURL:     *readyURL,
		ReadyTimeout: *readyTimeout,
		PIDFile:      *pidFile,
		LogFile:      *logFile,
	})
	if err != nil {
		response := errorResponse{
			Version: responseVersion,
			Status:  worker.StatusError,
			Reason:  err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			response.ReasonCode = supErr.Code
		}
		emitJSON(response)
		return 1
	}

	emitJSON(superviseResponse{
		Version:         responseVersion,
		Status:          worker.StatusOK,
		PID:             result.PID,
		ReadyURL:        result.ReadyURL,
		StatusCode:      result.Probe.StatusCode,
		ResponseHeaders: result.Probe.Headers,
		ResponseBody:    result.Probe.Body,
	})
	return 0
}

func runTerminate(args []string) int {
	fs := flag.NewFlagSet("terminate", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	pid := fs.Int("pid", 0, "pid to terminate")
	grace := fs.Duration("grace", 2*time.Second, "grace period before force kill")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 2
	}

	if err := worker.Terminate(*pid, *grace); err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 1
	}

	emitJSON(terminateResponse{
		Version: responseVersion,
		Status:  worker.StatusOK,
		PID:     *pid,
	})
	return 0
}

func runMonitor(args []string) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	pid := fs.Int("pid", 0, "pid to monitor")
	interval := fs.Duration("interval", 500*time.Millisecond, "polling interval")
	once := fs.Bool("once", false, "check status once and exit (no polling)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 2
	}

	if *once {
		s := worker.ProcessStatus(*pid)
		emitJSON(monitorResponse{
			Status: string(s),
			PID:    *pid,
		})
		return 0
	}

	if err := worker.Monitor(worker.MonitorOptions{PID: *pid, Interval: *interval}); err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 1
	}

	emitJSON(monitorResponse{
		Version: responseVersion,
		Status:  worker.StatusGone,
		PID:     *pid,
	})
	return 0
}

func runHash(args []string) int {
	fs := flag.NewFlagSet("hash", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	repoDir := fs.String("repo-dir", "", "repository root directory")
	profile := fs.String("profile", "", "runtime profile (node-http, go-http, worker-metrics)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 2
	}

	hash := worker.Hash(worker.HashOptions{
		RepoDir:        *repoDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
	})
	emitJSON(hashResponse{
		Version: responseVersion,
		Status:  worker.StatusOK,
		Hash:    hash,
	})
	return 0
}

func runControl(args []string) int {
	fs := flag.NewFlagSet("control", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	requestFile := fs.String("request-file", "", "path to control request JSON")
	responseFile := fs.String("response-file", "", "path to write control response JSON")
	pidFile := fs.String("pid-file", "", "path to pid file")
	readyURL := fs.String("ready-url", "", "readiness URL")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "readiness timeout")
	logFile := fs.String("log-file", "", "service log file")
	repoDir := fs.String("repo-dir", "", "repo directory for source hash")
	profile := fs.String("profile", "", "runtime profile for source hash")
	planFile := fs.String("plan-file", "", "service start plan JSON file")
	mirrordTarget := fs.String("mirrord-target", "", "mirrord target")
	serviceURL := fs.String("service-url", "", "service base URL")

	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	reqData, err := os.ReadFile(*requestFile)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read request: %v", err)})
		return 1
	}

	var req worker.ControlRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse request: %v", err)})
		return 1
	}
	command := []string{"mirrord", "exec", "--target", *mirrordTarget, "--", "dockhand", "exec-plan", "--plan-file", *planFile}
	resp := worker.ExecuteControl(context.Background(), &req, worker.ControlExecOptions{
		Command:      command,
		PIDFile:      *pidFile,
		ServiceURL:   *serviceURL,
		ReadyURL:     *readyURL,
		ReadyTimeout: *readyTimeout,
		LogFile:      *logFile,
		RepoDir:      *repoDir,
		Profile:      worker.RuntimeProfile(*profile),
	})

	respData, err := json.Marshal(resp)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("encode response: %v", err)})
		return 1
	}
	if err := os.WriteFile(*responseFile, respData, 0o644); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("write response: %v", err)})
		return 1
	}

	emitJSON(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}{Version: responseVersion, Status: worker.StatusOK})
	return 0
}

func runSubmitControl(args []string) int {
	fs := flag.NewFlagSet("submit-control", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	controlDir := fs.String("control-dir", "/artifacts/control", "control request directory")
	timeout := fs.Duration("timeout", 180*time.Second, "maximum time to wait for a control response")
	requestFile := fs.String("request-file", "", "path to request JSON file (default: read stdin)")
	requestJSON := fs.String("request-json", "", "request JSON payload")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	var reqData []byte
	var err error
	if strings.TrimSpace(*requestJSON) != "" {
		reqData = []byte(*requestJSON)
	} else if *requestFile != "" {
		reqData, err = os.ReadFile(*requestFile)
	} else {
		reqData, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read request: %v", err)})
		return 1
	}

	var req worker.ControlRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse request: %v", err)})
		return 1
	}
	if strings.TrimSpace(req.RequestID) == "" {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: "request_id is required"})
		return 1
	}
	if err := os.MkdirAll(*controlDir, 0o755); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("mkdir control dir: %v", err)})
		return 1
	}

	reqPath := filepath.Join(*controlDir, fmt.Sprintf("%s-%s.request", req.Action, req.RequestID))
	respPath := strings.TrimSuffix(reqPath, ".request") + ".response"
	_ = os.Remove(reqPath)
	_ = os.Remove(respPath)
	if err := os.WriteFile(reqPath, reqData, 0o644); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("write request: %v", err)})
		return 1
	}

	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(respPath); err == nil {
			fmt.Println(string(data))
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}

	timeoutResp := worker.NewControlErrorResponse(&req, "control.timeout", "timeout waiting for control response")
	respData, err := json.Marshal(timeoutResp)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("encode timeout response: %v", err)})
		return 1
	}
	fmt.Println(string(respData))
	return 0
}

func runRestart(args []string) int {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	pidFile := fs.String("pid-file", "", "path to pid file (reads old pid, writes new pid)")
	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")
	grace := fs.Duration("grace", 5*time.Second, "grace period for old process termination")
	repoDir := fs.String("repo-dir", "", "repository root directory for source hashing")
	profile := fs.String("profile", "", "runtime profile for source hashing")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emitJSON(errorResponse{
			Status: worker.StatusError,
			Reason: err.Error(),
		})
		return 2
	}

	result, err := worker.Restart(context.Background(), worker.RestartOptions{
		PIDFile:      *pidFile,
		Command:      cmdArgs,
		ReadyURL:     *readyURL,
		ReadyTimeout: *readyTimeout,
		LogFile:      *logFile,
		Grace:        *grace,
		RepoDir:      *repoDir,
		Profile:      worker.RuntimeProfile(*profile),
	})
	if err != nil {
		response := errorResponse{
			Version: responseVersion,
			Status:  worker.StatusError,
			Reason:  err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			response.ReasonCode = supErr.Code
		}
		emitJSON(response)
		return 1
	}

	emitJSON(restartResponse{
		Version:         responseVersion,
		Status:          worker.StatusOK,
		OldPID:          result.OldPID,
		NewPID:          result.NewPID,
		ReadyURL:        result.ReadyURL,
		OldCmdline:      result.OldCmdline,
		NewCmdline:      result.NewCmdline,
		OldSourceHash:   result.OldSourceHash,
		NewSourceHash:   result.NewSourceHash,
		StatusCode:      result.Probe.StatusCode,
		ResponseHeaders: result.Probe.Headers,
		ResponseBody:    result.Probe.Body,
	})
	return 0
}

func parseCommandArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	for i, arg := range args {
		if arg == "--" {
			if err := fs.Parse(args[:i]); err != nil {
				return nil, err
			}
			if len(args[i+1:]) == 0 {
				return nil, fmt.Errorf("command is required after --")
			}
			return args[i+1:], nil
		}
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() == 0 {
		return nil, fmt.Errorf("command is required after --")
	}
	return fs.Args(), nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
dockhand is a small process supervisor for agent-worker images.

Usage:
  dockhand run --payload-file PATH --workspace-dir PATH --artifacts-dir PATH
  dockhand exec-plan [--plan-file PATH] (reads plan JSON from stdin or file, executes it)
  dockhand supervise --ready-url URL [--ready-timeout DURATION] [--pid-file PATH] [--log-file PATH] -- <command...>
  dockhand terminate --pid PID [--grace DURATION]
  dockhand restart --pid-file PATH --ready-url URL [--ready-timeout DURATION] [--log-file PATH] [--grace DURATION] -- <command...>
  dockhand monitor --pid PID [--interval DURATION] [--once]
  dockhand hash --repo-dir PATH [--profile PROFILE]
  dockhand control --request-file PATH --response-file PATH [--pid-file PATH] [--ready-url URL] [--mirrord-target TARGET] [--plan-file PATH]
`))
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

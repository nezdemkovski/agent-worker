package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitJSON(startPlanResponse{
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
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
		emitJSON(errorResponse{Status: worker.StatusError, Reason: fmt.Sprintf("read plan: %v", err)})
		return 1
	}

	plan, err := worker.ParsePlanJSON(planData)
	if err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: fmt.Sprintf("parse plan: %v", err)})
		return 1
	}

	// ExecPlan replaces the process on the final exec step.
	// If it returns, either all non-exec steps completed or there was an error.
	if err := worker.ExecPlan(plan); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
			Status: worker.StatusError,
			Reason: err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			response.ReasonCode = supErr.Code
		}
		emitJSON(response)
		return 1
	}

	emitJSON(superviseResponse{
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
		Status: worker.StatusOK,
		PID:    *pid,
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
		Status: worker.StatusGone,
		PID:    *pid,
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
		Status: worker.StatusOK,
		Hash:   hash,
	})
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
			Status: worker.StatusError,
			Reason: err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			response.ReasonCode = supErr.Code
		}
		emitJSON(response)
		return 1
	}

	emitJSON(restartResponse{
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
  dockhand exec-plan [--plan-file PATH] (reads plan JSON from stdin or file, executes it)
  dockhand supervise --ready-url URL [--ready-timeout DURATION] [--pid-file PATH] [--log-file PATH] -- <command...>
  dockhand terminate --pid PID [--grace DURATION]
  dockhand restart --pid-file PATH --ready-url URL [--ready-timeout DURATION] [--log-file PATH] [--grace DURATION] -- <command...>
  dockhand monitor --pid PID [--interval DURATION] [--once]
  dockhand hash --repo-dir PATH [--profile PROFILE]
`))
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

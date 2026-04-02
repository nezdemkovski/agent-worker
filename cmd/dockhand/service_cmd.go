package main

import (
	"context"
	"errors"
	"time"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func runSupervise(args []string) int {
	fs := flagSet("supervise")
	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	pidFile := fs.String("pid-file", "", "path to write child pid")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
		response := errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()}
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
	fs := flagSet("terminate")
	pid := fs.Int("pid", 0, "pid to terminate")
	grace := fs.Duration("grace", 2*time.Second, "grace period before force kill")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	if err := worker.Terminate(*pid, *grace); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitJSON(terminateResponse{Version: responseVersion, Status: worker.StatusOK, PID: *pid})
	return 0
}

func runMonitor(args []string) int {
	fs := flagSet("monitor")
	pid := fs.Int("pid", 0, "pid to monitor")
	interval := fs.Duration("interval", 500*time.Millisecond, "polling interval")
	once := fs.Bool("once", false, "check status once and exit (no polling)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	if *once {
		s := worker.ProcessStatus(*pid)
		emitJSON(monitorResponse{Status: string(s), PID: *pid})
		return 0
	}

	if err := worker.Monitor(worker.MonitorOptions{PID: *pid, Interval: *interval}); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitJSON(monitorResponse{Version: responseVersion, Status: worker.StatusGone, PID: *pid})
	return 0
}

func runHash(args []string) int {
	fs := flagSet("hash")
	repoDir := fs.String("repo-dir", "", "repository root directory")
	profile := fs.String("profile", "", "runtime profile (node-http, go-http, worker-metrics)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	hash := worker.Hash(worker.HashOptions{
		RepoDir:        *repoDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
	})
	emitJSON(hashResponse{Version: responseVersion, Status: worker.StatusOK, Hash: hash})
	return 0
}

func runRestart(args []string) int {
	fs := flagSet("restart")
	pidFile := fs.String("pid-file", "", "path to pid file (reads old pid, writes new pid)")
	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")
	grace := fs.Duration("grace", 5*time.Second, "grace period for old process termination")
	repoDir := fs.String("repo-dir", "", "repository root directory for source hashing")
	profile := fs.String("profile", "", "runtime profile for source hashing")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emitJSON(errorResponse{Status: worker.StatusError, Reason: err.Error()})
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
		response := errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()}
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

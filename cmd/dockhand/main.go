package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: fmt.Sprintf("unknown subcommand %q", os.Args[1]),
		}, outputMode{})
		os.Exit(2)
	}
}

func runSupervise(args []string) int {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	jsonOutput := fs.Bool("json", false, "emit JSON output")
	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	pidFile := fs.String("pid-file", "", "path to write child pid")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
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
		fields := map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			fields[worker.KeyReasonCode] = string(supErr.Code)
		}
		emit(fields, outputMode{json: *jsonOutput})
		return 1
	}

	emit(map[string]string{
		worker.KeyStatus:   worker.StatusOK,
		worker.KeyPID:      fmt.Sprintf("%d", result.PID),
		worker.KeyReadyURL: result.ReadyURL,
	}, outputMode{json: *jsonOutput})
	return 0
}

func runTerminate(args []string) int {
	fs := flag.NewFlagSet("terminate", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	jsonOutput := fs.Bool("json", false, "emit JSON output")
	pid := fs.Int("pid", 0, "pid to terminate")
	grace := fs.Duration("grace", 2*time.Second, "grace period before force kill")
	if err := fs.Parse(args); err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 2
	}

	if err := worker.Terminate(*pid, *grace); err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 1
	}

	emit(map[string]string{
		worker.KeyStatus: worker.StatusOK,
		worker.KeyPID:    fmt.Sprintf("%d", *pid),
	}, outputMode{json: *jsonOutput})
	return 0
}

func runMonitor(args []string) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	jsonOutput := fs.Bool("json", false, "emit JSON output")
	pid := fs.Int("pid", 0, "pid to monitor")
	interval := fs.Duration("interval", 500*time.Millisecond, "polling interval")
	once := fs.Bool("once", false, "check status once and exit (no polling)")
	if err := fs.Parse(args); err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 2
	}

	if *once {
		s := worker.ProcessStatus(*pid)
		emit(map[string]string{
			worker.KeyStatus: string(s),
			worker.KeyPID:    fmt.Sprintf("%d", *pid),
		}, outputMode{json: *jsonOutput})
		return 0
	}

	if err := worker.Monitor(worker.MonitorOptions{PID: *pid, Interval: *interval}); err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 1
	}

	emit(map[string]string{
		worker.KeyStatus: worker.StatusGone,
		worker.KeyPID:    fmt.Sprintf("%d", *pid),
	}, outputMode{json: *jsonOutput})
	return 0
}

func runHash(args []string) int {
	fs := flag.NewFlagSet("hash", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	jsonOutput := fs.Bool("json", false, "emit JSON output")
	repoDir := fs.String("repo-dir", "", "repository root directory")
	profile := fs.String("profile", "", "runtime profile (node-http, go-http, worker-metrics)")
	if err := fs.Parse(args); err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 2
	}

	hash := worker.Hash(worker.HashOptions{
		RepoDir:        *repoDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
	})
	emit(map[string]string{
		worker.KeyStatus: worker.StatusOK,
		worker.KeyHash:   hash,
	}, outputMode{json: *jsonOutput})
	return 0
}

func runRestart(args []string) int {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	jsonOutput := fs.Bool("json", false, "emit JSON output")
	pidFile := fs.String("pid-file", "", "path to pid file (reads old pid, writes new pid)")
	readyURL := fs.String("ready-url", "", "readiness URL to poll")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "maximum readiness wait time")
	logFile := fs.String("log-file", "", "path to append child stdout/stderr")
	grace := fs.Duration("grace", 5*time.Second, "grace period for old process termination")

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		emit(map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}, outputMode{json: *jsonOutput})
		return 2
	}

	result, err := worker.Restart(context.Background(), worker.RestartOptions{
		PIDFile:      *pidFile,
		Command:      cmdArgs,
		ReadyURL:     *readyURL,
		ReadyTimeout: *readyTimeout,
		LogFile:      *logFile,
		Grace:        *grace,
	})
	if err != nil {
		fields := map[string]string{
			worker.KeyStatus: worker.StatusError,
			worker.KeyReason: err.Error(),
		}
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			fields[worker.KeyReasonCode] = string(supErr.Code)
		}
		emit(fields, outputMode{json: *jsonOutput})
		return 1
	}

	emit(map[string]string{
		worker.KeyStatus:     worker.StatusOK,
		worker.KeyOldPID:     fmt.Sprintf("%d", result.OldPID),
		worker.KeyNewPID:     fmt.Sprintf("%d", result.NewPID),
		worker.KeyReadyURL:   result.ReadyURL,
		worker.KeyOldCmdline: result.OldCmdline,
		worker.KeyNewCmdline: result.NewCmdline,
	}, outputMode{json: *jsonOutput})
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
  dockhand supervise [--json] --ready-url URL [--ready-timeout DURATION] [--pid-file PATH] [--log-file PATH] -- <command...>
  dockhand terminate [--json] --pid PID [--grace DURATION]
  dockhand restart [--json] --pid-file PATH --ready-url URL [--ready-timeout DURATION] [--log-file PATH] [--grace DURATION] -- <command...>
  dockhand monitor [--json] --pid PID [--interval DURATION] [--once]
  dockhand hash [--json] --repo-dir PATH [--profile PROFILE]
`))
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

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
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, fmt.Sprintf("unknown subcommand %q", os.Args[1]))
		os.Exit(2)
	}
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
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
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
		printKV(worker.KeyStatus, worker.StatusError)
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			printKV(worker.KeyReasonCode, string(supErr.Code))
		}
		printKV(worker.KeyReason, err.Error())
		return 1
	}

	printKV(worker.KeyStatus, worker.StatusOK)
	printKV(worker.KeyPID, fmt.Sprintf("%d", result.PID))
	printKV(worker.KeyReadyURL, result.ReadyURL)
	return 0
}

func runTerminate(args []string) int {
	fs := flag.NewFlagSet("terminate", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	pid := fs.Int("pid", 0, "pid to terminate")
	grace := fs.Duration("grace", 2*time.Second, "grace period before force kill")
	if err := fs.Parse(args); err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
		return 2
	}

	if err := worker.Terminate(*pid, *grace); err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
		return 1
	}

	printKV(worker.KeyStatus, worker.StatusOK)
	printKV(worker.KeyPID, fmt.Sprintf("%d", *pid))
	return 0
}

func runMonitor(args []string) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	pid := fs.Int("pid", 0, "pid to monitor")
	interval := fs.Duration("interval", 500*time.Millisecond, "polling interval")
	once := fs.Bool("once", false, "check status once and exit (no polling)")
	if err := fs.Parse(args); err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
		return 2
	}

	if *once {
		s := worker.ProcessStatus(*pid)
		printKV(worker.KeyStatus, string(s))
		printKV(worker.KeyPID, fmt.Sprintf("%d", *pid))
		return 0
	}

	if err := worker.Monitor(worker.MonitorOptions{PID: *pid, Interval: *interval}); err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
		return 1
	}

	printKV(worker.KeyStatus, worker.StatusGone)
	printKV(worker.KeyPID, fmt.Sprintf("%d", *pid))
	return 0
}

func runHash(args []string) int {
	fs := flag.NewFlagSet("hash", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	repoDir := fs.String("repo-dir", "", "repository root directory")
	profile := fs.String("profile", "", "runtime profile (node-http, go-http, worker-metrics)")
	if err := fs.Parse(args); err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
		return 2
	}

	hash := worker.Hash(worker.HashOptions{
		RepoDir:        *repoDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
	})
	printKV(worker.KeyStatus, worker.StatusOK)
	printKV(worker.KeyHash, hash)
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

	cmdArgs, err := parseCommandArgs(fs, args)
	if err != nil {
		printKV(worker.KeyStatus, worker.StatusError)
		printKV(worker.KeyReason, err.Error())
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
		printKV(worker.KeyStatus, worker.StatusError)
		var supErr *worker.SuperviseError
		if errors.As(err, &supErr) {
			printKV(worker.KeyReasonCode, string(supErr.Code))
		}
		printKV(worker.KeyReason, err.Error())
		return 1
	}

	printKV(worker.KeyStatus, worker.StatusOK)
	printKV(worker.KeyOldPID, fmt.Sprintf("%d", result.OldPID))
	printKV(worker.KeyNewPID, fmt.Sprintf("%d", result.NewPID))
	printKV(worker.KeyReadyURL, result.ReadyURL)
	printKV(worker.KeyOldCmdline, result.OldCmdline)
	printKV(worker.KeyNewCmdline, result.NewCmdline)
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
  dockhand supervise --ready-url URL [--ready-timeout DURATION] [--pid-file PATH] [--log-file PATH] -- <command...>
  dockhand terminate --pid PID [--grace DURATION]
  dockhand restart --pid-file PATH --ready-url URL [--ready-timeout DURATION] [--log-file PATH] [--grace DURATION] -- <command...>
  dockhand monitor --pid PID [--interval DURATION] [--once]
  dockhand hash --repo-dir PATH [--profile PROFILE]
`))
}

func printKV(key, value string) {
	fmt.Printf("%s=%s\n", key, value)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

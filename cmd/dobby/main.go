package main

import (
	"fmt"
	"os"
	"strings"

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

func printUsage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
dobby is a small process supervisor for agent-worker images.

Usage:
  dobby run --payload-file PATH --workspace-dir PATH --artifacts-dir PATH
  dobby exec-plan [--plan-file PATH] (reads plan JSON from stdin or file, executes it)
  dobby supervise --ready-url URL [--ready-timeout DURATION] [--pid-file PATH] [--log-file PATH] -- <command...>
  dobby terminate --pid PID [--grace DURATION]
  dobby restart --pid-file PATH --ready-url URL [--ready-timeout DURATION] [--log-file PATH] [--grace DURATION] -- <command...>
  dobby monitor --pid PID [--interval DURATION] [--once]
  dobby hash --repo-dir PATH [--profile PROFILE]
  dobby control --request-file PATH --response-file PATH [--pid-file PATH] [--ready-url URL] [--mirrord-target TARGET] [--plan-file PATH]
`))
}

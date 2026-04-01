package worker

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// RestartOptions configures a Restart call.
type RestartOptions struct {
	PIDFile      string        // path to read old pid from and write new pid to
	Command      []string      // new process command
	ReadyURL     string        // readiness URL to poll
	ReadyTimeout time.Duration // maximum readiness wait time
	LogFile      string        // path to append child stdout/stderr
	Grace        time.Duration // grace period for old process termination
}

// RestartResult holds the outcome of a successful Restart call.
type RestartResult struct {
	OldPID   int
	NewPID   int
	ReadyURL string
}

// Restart terminates the process recorded in opts.PIDFile (if any), then
// starts a new process and waits for HTTP readiness. The pid file is
// overwritten with the new pid on success. A missing or invalid pid file is
// treated as "no old process"; the restart proceeds normally.
func Restart(ctx context.Context, opts RestartOptions) (*RestartResult, error) {
	if opts.Grace <= 0 {
		opts.Grace = 5 * time.Second
	}

	oldPID := readPIDFile(opts.PIDFile)
	if oldPID > 0 {
		// best-effort: process may already be dead
		_ = terminateProcessTree(oldPID, opts.Grace)
	}

	result, err := Supervise(ctx, SuperviseOptions{
		Command:      opts.Command,
		ReadyURL:     opts.ReadyURL,
		ReadyTimeout: opts.ReadyTimeout,
		PIDFile:      opts.PIDFile,
		LogFile:      opts.LogFile,
	})
	if err != nil {
		return nil, err
	}

	return &RestartResult{OldPID: oldPID, NewPID: result.PID, ReadyURL: result.ReadyURL}, nil
}

func readPIDFile(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

package worker

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

type RestartOptions struct {
	PIDFile      string
	Command      []string
	ReadyURL     string
	ReadyTimeout time.Duration
	LogFile      string
	Grace        time.Duration
	RepoDir      string
	Profile      RuntimeProfile
}

type RestartResult struct {
	OldPID        int
	NewPID        int
	ReadyURL      string
	OldCmdline    string
	NewCmdline    string
	OldSourceHash string
	NewSourceHash string
	Probe         ProbeResult
}

func Restart(ctx context.Context, opts RestartOptions) (*RestartResult, error) {
	if opts.Grace <= 0 {
		opts.Grace = 5 * time.Second
	}

	oldPID := readPIDFile(opts.PIDFile)
	oldCmdline := pidCmdline(oldPID)
	oldSourceHash := Hash(HashOptions{
		RepoDir:        opts.RepoDir,
		RuntimeProfile: opts.Profile,
	})
	if oldPID > 0 {
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

	return &RestartResult{
		OldPID:        oldPID,
		NewPID:        result.PID,
		ReadyURL:      result.ReadyURL,
		OldCmdline:    oldCmdline,
		NewCmdline:    pidCmdline(result.PID),
		OldSourceHash: oldSourceHash,
		NewSourceHash: Hash(HashOptions{
			RepoDir:        opts.RepoDir,
			RuntimeProfile: opts.Profile,
		}),
		Probe: result.Probe,
	}, nil
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

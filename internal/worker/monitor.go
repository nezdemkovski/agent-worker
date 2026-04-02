package worker

import (
	"fmt"
	"time"
)

type MonitorOptions struct {
	PID      int
	Interval time.Duration
}

func ProcessStatus(pid int) ProcessState {
	if pid <= 0 {
		return StateGone
	}
	running, _ := processRunning(pid)
	if !running {
		return StateGone
	}
	return StateRunning
}

func Monitor(opts MonitorOptions) error {
	if opts.PID <= 0 {
		return fmt.Errorf("invalid pid %d", opts.PID)
	}
	if opts.Interval <= 0 {
		opts.Interval = 500 * time.Millisecond
	}
	for {
		running, _ := processRunning(opts.PID)
		if !running {
			return nil
		}
		time.Sleep(opts.Interval)
	}
}

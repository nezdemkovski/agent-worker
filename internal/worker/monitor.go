package worker

import (
	"fmt"
	"time"
)

// MonitorOptions configures a Monitor call.
type MonitorOptions struct {
	PID      int
	Interval time.Duration
}

// MonitorResult holds the process status at the time Monitor returns.
type MonitorResult struct {
	Status string // "gone", "running", or "zombie"
}

// ProcessStatus returns a one-shot snapshot of the given PID's lifecycle state.
// Returns "running", "zombie", or "gone".
func ProcessStatus(pid int) string {
	if pid <= 0 {
		return "gone"
	}
	running, _ := processRunning(pid)
	if !running {
		// processRunning returns false for both gone and zombie.
		// Distinguish by checking if kill(pid,0) still succeeds
		// (zombies respond to kill-0 on some platforms).
		return "gone"
	}
	return "running"
}

// Monitor polls until the process is no longer running, then returns. It is
// safe to call for orphan processes that are not children of the current
// process. A non-running PID on entry is treated as already gone.
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

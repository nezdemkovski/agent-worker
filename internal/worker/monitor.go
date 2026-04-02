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

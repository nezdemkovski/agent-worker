package worker

import (
	"fmt"
	"time"
)

// Terminate sends SIGTERM to the process group of pid, then waits up to grace
// for the process to exit before sending SIGKILL.
func Terminate(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if grace <= 0 {
		grace = 2 * time.Second
	}
	return terminateProcessTree(pid, grace)
}

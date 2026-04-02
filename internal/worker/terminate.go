package worker

import (
	"fmt"
	"time"
)

func Terminate(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if grace <= 0 {
		grace = 2 * time.Second
	}
	return terminateProcessTree(pid, grace)
}

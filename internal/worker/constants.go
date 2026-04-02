package worker

type ProcessState string

const (
	StateRunning ProcessState = "running"
	StateGone    ProcessState = "gone"
)

type ReasonCode string

const (
	ReasonExitedBeforeReady ReasonCode = "exited_before_ready"
	ReasonTimeout           ReasonCode = "timeout"
)

type RuntimeProfile string

const (
	ProfileNodeHTTP      RuntimeProfile = "node-http"
	ProfileGoHTTP        RuntimeProfile = "go-http"
	ProfileWorkerMetrics RuntimeProfile = "worker-metrics"
)

const (
	StatusOK    = "ok"
	StatusError = "error"
	StatusGone  = "gone"
)

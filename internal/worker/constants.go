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

type StartStrategy string

const (
	StrategyGoRun    StartStrategy = "go-run"
	StrategyAir      StartStrategy = "air"
	StrategyNpmAuto  StartStrategy = "npm-auto"
	StrategyPnpmDev  StartStrategy = "pnpm-dev"
	StrategyPnpmStart StartStrategy = "pnpm-start"
)

const (
	StatusOK    = "ok"
	StatusError = "error"
	StatusGone  = "gone"
)

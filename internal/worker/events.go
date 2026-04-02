package worker

import "time"

type EventLevel string

const (
	LevelInfo  EventLevel = "info"
	LevelWarn  EventLevel = "warn"
	LevelError EventLevel = "error"
)

type EventCode string

// Service lifecycle events.
const (
	CodeServiceTarget            EventCode = "service.target"
	CodeServiceStart             EventCode = "service.start"
	CodeServiceStartStrategy     EventCode = "service.start_strategy"
	CodeServiceStartCommand      EventCode = "service.start_command"
	CodeServiceReady             EventCode = "service.ready"
	CodeServiceReadyTimeout      EventCode = "service.ready.timeout"
	CodeServiceExitedBeforeReady EventCode = "service.exited_before_ready"
	CodeServicePlanFail          EventCode = "service.plan.fail"
	CodeServiceSkip              EventCode = "service.skip"
	CodeServiceFail              EventCode = "service.fail"
	CodeServiceRecover           EventCode = "service.recover"
	CodeServiceRecoverFail       EventCode = "service.recover.fail"
	CodeServiceSessionFail       EventCode = "service.session.fail"
)

// Mirrord execution events.
const (
	CodeMirrordExec        EventCode = "mirrord.exec"
	CodeMirrordFail        EventCode = "mirrord.fail"
	CodeMirrordSkip        EventCode = "mirrord.skip"
	CodeMirrordGoTest      EventCode = "mirrord.go_test"
	CodeMirrordPnpmTest    EventCode = "mirrord.pnpm_test"
	CodeMirrordSessionFail EventCode = "mirrord.session.fail"
	CodeMirrordSmokeProbe  EventCode = "mirrord.smoke_probe"
)

// Verification events.
const (
	CodeVerificationServiceReady EventCode = "verification.service_ready"
	CodeVerificationFail         EventCode = "verification.fail"
	CodeVerificationSessionFail  EventCode = "verification.session.fail"
	CodeVerificationSmoke        EventCode = "verification.smoke"
	CodeVerificationOK           EventCode = "verification.ok"
	CodeVerificationSkip         EventCode = "verification.skip"
	CodeVerificationGoTest       EventCode = "verification.go_test"
	CodeVerificationPlanPnpmTest EventCode = "verification.plan_pnpm_test"
)

// Bootstrap events.
const (
	CodeRepoCheckout  EventCode = "repo.checkout"
	CodeRepoClone     EventCode = "repo.clone"
	CodeRepoBranch    EventCode = "repo.branch"
	CodeRepoBootstrap EventCode = "repo.bootstrap"
	CodeRepoFail      EventCode = "repo.fail"
)

// Control action events.
const (
	CodeControlReceived  EventCode = "control.received"
	CodeControlCompleted EventCode = "control.completed"
	CodeControlFailed    EventCode = "control.failed"
)

type Event struct {
	Time    string            `json:"time"`
	Kind    string            `json:"kind"`
	Level   EventLevel        `json:"level"`
	Code    EventCode         `json:"code"`
	Message string            `json:"message"`
	Service string            `json:"service,omitempty"`
	Repo    string            `json:"repo,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

type EventLog struct {
	Version int     `json:"version"`
	Events  []Event `json:"events"`
}

func NewEvent(code EventCode, level EventLevel, message string) Event {
	kind := ""
	for i, c := range string(code) {
		if c == '.' {
			kind = string(code)[:i]
			break
		}
	}
	return Event{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Kind:    kind,
		Level:   level,
		Code:    code,
		Message: message,
	}
}

func NewEventLog() *EventLog {
	return &EventLog{Version: 1, Events: []Event{}}
}

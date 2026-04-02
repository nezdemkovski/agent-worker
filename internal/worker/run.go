package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type RepoSpec struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Path string `json:"path"`
}

type ServiceSpec struct {
	Name                    string `json:"name"`
	ServiceName             string `json:"service_name"`
	Kind                    string `json:"kind,omitempty"`
	Repo                    string `json:"repo,omitempty"`
	Workdir                 string `json:"workdir,omitempty"`
	RuntimeProfile          string `json:"runtime_profile,omitempty"`
	Target                  string `json:"target,omitempty"`
	Entrypoint              string `json:"entrypoint,omitempty"`
	ReadinessPath           string `json:"readiness_path,omitempty"`
	StartStrategy           string `json:"start_strategy,omitempty"`
	DevPort                 int    `json:"dev_port,omitempty"`
	ReadinessTimeoutSeconds int    `json:"readiness_timeout_seconds,omitempty"`
}

type WorkerPayload struct {
	SessionID           string        `json:"session_id"`
	RunID               string        `json:"run_id"`
	Namespace           string        `json:"namespace"`
	Task                string        `json:"task"`
	Branch              string        `json:"branch"`
	WorkerImage         string        `json:"worker_image"`
	Services            []string      `json:"services"`
	ServiceSpecs        []ServiceSpec `json:"service_specs"`
	ServicePlan         []string      `json:"service_plan"`
	VerificationProfile string        `json:"verification_profile"`
	VerificationPlan    []string      `json:"verification_plan"`
	Mode                string        `json:"mode"`
	Repos               []string      `json:"repos"`
	RepoSpecs           []RepoSpec    `json:"repo_specs"`
}

type RunOptions struct {
	PayloadPath         string
	WorkspaceDir        string
	ArtifactsDir        string
	DefaultServicePort  string
	DefaultReadyPath    string
	DefaultReadyTimeout time.Duration
	PlanCommandBuilder  func(planFile, target string) []string
}

func Run(ctx context.Context, opts RunOptions) error {
	if err := prepareWorkerEnvironment(); err != nil {
		return err
	}
	payload, err := readWorkerPayload(opts.PayloadPath)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(payload.Mode) {
	case string(RunModeService):
		return runServiceMode(ctx, payload, opts)
	case string(RunModeVerify), "":
		return runVerifyMode(ctx, payload, opts)
	default:
		return fmt.Errorf("unsupported run mode %q", payload.Mode)
	}
}

func writeServiceReady(arts workerArtifacts, service, target, serviceURL, readyURL string, probe ProbeResult) error {
	_ = os.WriteFile(arts.ServiceURL, []byte(serviceURL+"\n"), 0o644)
	_ = os.WriteFile(arts.ServiceProbeHdrs, []byte(probe.Headers), 0o644)
	_ = os.WriteFile(arts.ServiceProbeBody, []byte(probe.Body), 0o644)
	payload := map[string]any{
		"version":   1,
		"service":   service,
		"url":       serviceURL,
		"ready_url": readyURL,
		"target":    target,
		"status":    "ready",
		"probe": map[string]any{
			"status_code": probe.StatusCode,
			"headers":     probe.Headers,
			"body":        probe.Body,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(arts.ServiceReady, append(data, '\n'), 0o644)
}

func withService(event Event, service string, details map[string]string) Event {
	event.Service = service
	if len(details) > 0 {
		event.Details = details
	}
	return event
}

func eventWithRepo(event Event, repo string) Event {
	event.Repo = repo
	return event
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

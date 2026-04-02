package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type workerArtifacts struct {
	WorkspaceManifest  string
	ClonePlan          string
	CheckoutResult     string
	BranchResult       string
	BootstrapPlan      string
	BootstrapResult    string
	ServicePlan        string
	ServiceResult      string
	MirrordPlan        string
	MirrordResult      string
	VerificationPlan   string
	VerificationResult string
	ServiceURL         string
	ServiceReady       string
	ServiceLog         string
	ServicePID         string
	ServiceProbeHdrs   string
	ServiceProbeBody   string
	ControlDir         string
	ServiceStartPlan   string
}

func initWorkerArtifacts(dir string) (workerArtifacts, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return workerArtifacts{}, err
	}
	arts := workerArtifacts{
		WorkspaceManifest:  filepath.Join(dir, "workspace.txt"),
		ClonePlan:          filepath.Join(dir, "clone-plan.txt"),
		CheckoutResult:     filepath.Join(dir, "checkout-result.txt"),
		BranchResult:       filepath.Join(dir, "branch-result.txt"),
		BootstrapPlan:      filepath.Join(dir, "bootstrap-plan.txt"),
		BootstrapResult:    filepath.Join(dir, "bootstrap-result.json"),
		ServicePlan:        filepath.Join(dir, "service-plan.txt"),
		ServiceResult:      filepath.Join(dir, "service-result.json"),
		MirrordPlan:        filepath.Join(dir, "mirrord-plan.txt"),
		MirrordResult:      filepath.Join(dir, "mirrord-result.json"),
		VerificationPlan:   filepath.Join(dir, "verification-plan.txt"),
		VerificationResult: filepath.Join(dir, "verification-result.json"),
		ServiceURL:         filepath.Join(dir, "service-url.txt"),
		ServiceReady:       filepath.Join(dir, "service-ready.json"),
		ServiceLog:         filepath.Join(dir, "service-log.txt"),
		ServicePID:         filepath.Join(dir, "service-pid.txt"),
		ServiceProbeHdrs:   filepath.Join(dir, "service-probe.headers"),
		ServiceProbeBody:   filepath.Join(dir, "service-probe.txt"),
		ControlDir:         filepath.Join(dir, "control"),
		ServiceStartPlan:   filepath.Join(dir, "service-start-plan.json"),
	}
	for _, path := range []string{arts.WorkspaceManifest, arts.ClonePlan, arts.CheckoutResult, arts.BranchResult, arts.BootstrapPlan, arts.ServicePlan, arts.MirrordPlan, arts.VerificationPlan, arts.ServiceLog, arts.ServiceURL, arts.ServiceProbeHdrs, arts.ServiceProbeBody, arts.ServicePID} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return workerArtifacts{}, err
		}
	}
	if err := os.MkdirAll(arts.ControlDir, 0o755); err != nil {
		return workerArtifacts{}, err
	}
	return arts, nil
}

func readWorkerPayload(path string) (*WorkerPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	var payload WorkerPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	return &payload, nil
}

func writeLines(path string, lines []string) {
	if len(lines) == 0 {
		_ = os.WriteFile(path, nil, 0o644)
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func appendLine(path, line string) {
	if line == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

func appendLines(path string, lines []string) {
	for _, line := range lines {
		appendLine(path, line)
	}
}

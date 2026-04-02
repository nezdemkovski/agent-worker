package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

type eventLogFile struct {
	path string
	log  *EventLog
	out  io.Writer
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

func runServiceMode(ctx context.Context, payload *WorkerPayload, opts RunOptions) error {
	arts, err := initWorkerArtifacts(opts.ArtifactsDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.WorkspaceDir, 0o755); err != nil {
		return fmt.Errorf("mkdir workspace: %w", err)
	}

	serviceResult, err := newEventLogFile(arts.ServiceResult, os.Stdout)
	if err != nil {
		return err
	}
	mirrordResult, err := newEventLogFile(arts.MirrordResult, os.Stdout)
	if err != nil {
		return err
	}
	verificationResult, err := newEventLogFile(arts.VerificationResult, os.Stdout)
	if err != nil {
		return err
	}
	bootstrapResult, err := newEventLogFile(arts.BootstrapResult, os.Stdout)
	if err != nil {
		return err
	}

	writeLines(arts.ServicePlan, payload.ServicePlan)
	writeLines(arts.VerificationPlan, payload.VerificationPlan)

	repoSpecs := map[string]RepoSpec{}
	for _, spec := range payload.RepoSpecs {
		repoSpecs[spec.Name] = spec
	}
	serviceSpecs := map[string]ServiceSpec{}
	for _, spec := range payload.ServiceSpecs {
		serviceSpecs[spec.Name] = spec
	}

	for _, repo := range payload.Repos {
		spec := repoSpecs[repo]
		repoDir := spec.Path
		if strings.TrimSpace(repoDir) == "" {
			repoDir = filepath.Join(opts.WorkspaceDir, repo)
		}
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return fmt.Errorf("mkdir repo dir: %w", err)
		}
		appendLine(arts.WorkspaceManifest, repo)
		result, err := BootstrapRepo(BootstrapRepoOptions{
			Repo:         repo,
			RepoDir:      repoDir,
			RemoteURL:    spec.URL,
			Branch:       payload.Branch,
			RunMode:      RunModeService,
			PNPMStoreDir: os.Getenv("PNPM_STORE_DIR"),
			PNPMStateDir: os.Getenv("PNPM_STATE_DIR"),
		})
		appendLines(arts.ClonePlan, result.ClonePlan)
		appendLines(arts.CheckoutResult, result.CheckoutResult)
		appendLines(arts.BranchResult, result.BranchResult)
		appendLines(arts.BootstrapPlan, result.BootstrapPlan)
		appendBootstrapTimeline(bootstrapResult, repo, repoDir, result)
		if err != nil {
			appendLine(arts.CheckoutResult, fmt.Sprintf("FAIL %s: %v", repo, err))
			return err
		}
	}

	if len(payload.Services) == 0 {
		_ = serviceResult.Append(NewEvent(CodeServiceSkip, LevelWarn, "no service requested for service mode"))
		return fmt.Errorf("no service requested for service mode")
	}
	serviceName := payload.Services[0]
	spec, ok := serviceSpecs[serviceName]
	if !ok {
		return fmt.Errorf("missing service spec for %s", serviceName)
	}

	repoDir := spec.Workdir
	if strings.TrimSpace(repoDir) == "" {
		repoDir = filepath.Join(opts.WorkspaceDir, serviceName)
	}
	runtimeProfile := RuntimeProfile(firstNonEmpty(spec.RuntimeProfile, string(ProfileGoHTTP)))
	entrypoint := firstNonEmpty(spec.Entrypoint, fmt.Sprintf("./cmd/%s/main.go", serviceName))
	startStrategy := spec.StartStrategy
	target := firstNonEmpty(spec.Target, "deploy/"+serviceName)
	servicePort := spec.DevPort
	if servicePort == 0 && opts.DefaultServicePort != "" {
		fmt.Sscanf(opts.DefaultServicePort, "%d", &servicePort)
	}
	if servicePort == 0 {
		servicePort = 31140
	}
	readyPath := firstNonEmpty(spec.ReadinessPath, opts.DefaultReadyPath, "/healthz")
	readyTimeout := time.Duration(spec.ReadinessTimeoutSeconds) * time.Second
	if readyTimeout == 0 {
		if opts.DefaultReadyTimeout > 0 {
			readyTimeout = opts.DefaultReadyTimeout
		} else {
			readyTimeout = 180 * time.Second
		}
	}
	serviceURL := fmt.Sprintf("http://127.0.0.1:%d", servicePort)
	readyURL := serviceURL + readyPath

	_ = serviceResult.Append(withService(NewEvent(CodeServiceTarget, LevelInfo, target), serviceName, map[string]string{"target": target}))
	plan, err := PlanStart(StartPlanOptions{
		Service:        serviceName,
		WorkDir:        repoDir,
		RuntimeProfile: runtimeProfile,
		EntryPoint:     entrypoint,
		StartStrategy:  startStrategy,
		Port:           fmt.Sprintf("%d", servicePort),
	})
	if err != nil {
		_ = serviceResult.Append(withService(NewEvent(CodeServicePlanFail, LevelError, err.Error()), serviceName, nil))
		return err
	}
	planJSON, err := FormatPlanJSON(plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(arts.ServiceStartPlan, []byte(planJSON), 0o644); err != nil {
		return fmt.Errorf("write service start plan: %w", err)
	}
	appendLine(arts.MirrordPlan, fmt.Sprintf("mirrord exec --target %s -- dockhand exec-plan --plan-file %s", target, arts.ServiceStartPlan))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStart, LevelInfo, "mirrord-backed local service on "+serviceURL), serviceName, map[string]string{"url": serviceURL, "target": target}))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStartStrategy, LevelInfo, plan.Strategy), serviceName, nil))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStartCommand, LevelInfo, plan.Description), serviceName, nil))

	commandBuilder := opts.PlanCommandBuilder
	if commandBuilder == nil {
		commandBuilder = func(planFile, target string) []string {
			return []string{"mirrord", "exec", "--target", target, "--", "dockhand", "exec-plan", "--plan-file", planFile}
		}
	}
	command := commandBuilder(arts.ServiceStartPlan, target)
	superviseResult, err := superviseServiceSession(ctx, payload, serviceName, target, command, readyURL, readyTimeout, arts, serviceResult, mirrordResult, verificationResult)
	if err != nil {
		return err
	}

	if err := writeServiceReady(arts, serviceName, target, serviceURL, readyURL, superviseResult.Probe); err != nil {
		return err
	}
	_ = serviceResult.Append(withService(NewEvent(CodeServiceReady, LevelInfo, readyURL), serviceName, map[string]string{"ready_url": readyURL}))
	_ = mirrordResult.Append(withService(NewEvent(CodeMirrordExec, LevelInfo, "service readiness probe"), serviceName, nil))
	_ = verificationResult.Append(withService(NewEvent(CodeVerificationServiceReady, LevelInfo, readyURL), serviceName, map[string]string{"ready_url": readyURL}))
	_ = verificationResult.Append(NewEvent(CodeVerificationSmoke, LevelInfo, "service mode"))
	_ = verificationResult.Append(NewEvent(CodeVerificationOK, LevelInfo, "service ready artifact present"))

	currentPID := superviseResult.PID
	for {
		select {
		case <-ctx.Done():
			_ = Terminate(currentPID, 2*time.Second)
			return ctx.Err()
		default:
		}
		if err := processControlRequests(ctx, arts, serviceName, repoDir, runtimeProfile, serviceURL, readyURL, readyTimeout, target, commandBuilder, serviceResult, mirrordResult, verificationResult, &currentPID); err != nil {
			return err
		}
		if ProcessStatus(currentPID) != StateRunning {
			_ = serviceResult.Append(withService(NewEvent(CodeServiceSessionFail, LevelError, fmt.Sprintf("process %d exited unexpectedly", currentPID)), serviceName, map[string]string{"pid": fmt.Sprintf("%d", currentPID)}))
			_ = verificationResult.Append(NewEvent(CodeVerificationSessionFail, LevelError, "process exited unexpectedly"))
			_ = mirrordResult.Append(NewEvent(CodeMirrordSessionFail, LevelError, "process exited unexpectedly"))
			return fmt.Errorf("process %d exited unexpectedly", currentPID)
		}
		time.Sleep(1 * time.Second)
	}
}

func superviseServiceSession(ctx context.Context, payload *WorkerPayload, serviceName, target string, command []string, readyURL string, readyTimeout time.Duration, arts workerArtifacts, serviceResult, mirrordResult, verificationResult *eventLogFile) (*SuperviseResult, error) {
	retriedRecovery := false
	for {
		superviseResult, err := Supervise(ctx, SuperviseOptions{
			Command:      command,
			ReadyURL:     readyURL,
			ReadyTimeout: readyTimeout,
			PIDFile:      arts.ServicePID,
			LogFile:      arts.ServiceLog,
		})
		if err == nil {
			return superviseResult, nil
		}

		var supErr *SuperviseError
		if errors.As(err, &supErr) && supErr.Code == ReasonExitedBeforeReady {
			if !retriedRecovery && isDirtyIptablesLog(arts.ServiceLog) {
				_ = serviceResult.Append(withService(NewEvent(CodeServiceRecover, LevelWarn, "detected dirty mirrord iptables state"), serviceName, nil))
				if recoverMirrordTarget(ctx, payload.Namespace, serviceName, target, arts.ServiceLog, serviceResult) {
					retriedRecovery = true
					continue
				}
				_ = serviceResult.Append(withService(NewEvent(CodeServiceRecoverFail, LevelError, "automatic mirrord target recovery failed"), serviceName, nil))
			}
			_ = serviceResult.Append(withService(NewEvent(CodeServiceExitedBeforeReady, LevelError, "service process exited before readiness probe passed"), serviceName, nil))
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "service process exited before readiness"), serviceName, nil))
			_ = verificationResult.Append(withService(NewEvent(CodeVerificationFail, LevelError, "service process exited before readiness"), serviceName, nil))
		} else {
			_ = serviceResult.Append(withService(NewEvent(CodeServiceReadyTimeout, LevelError, "service did not become ready at "+readyURL), serviceName, nil))
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "service readiness probe"), serviceName, nil))
			_ = verificationResult.Append(withService(NewEvent(CodeVerificationFail, LevelError, "service did not become ready"), serviceName, nil))
		}
		return nil, err
	}
}

func processControlRequests(ctx context.Context, arts workerArtifacts, serviceName, repoDir string, profile RuntimeProfile, serviceURL, readyURL string, readyTimeout time.Duration, target string, commandBuilder func(planFile, target string) []string, serviceResult, mirrordResult, verificationResult *eventLogFile, currentPID *int) error {
	matches, err := filepath.Glob(filepath.Join(arts.ControlDir, "*.request"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for _, requestFile := range matches {
		reqData, err := os.ReadFile(requestFile)
		if err != nil {
			return err
		}
		var req ControlRequest
		if err := json.Unmarshal(reqData, &req); err != nil {
			return err
		}
		message, details := describeControlRequest(&req)
		appendControlTimeline(serviceResult, &req, "received", message, details)
		resp := ExecuteControl(ctx, &req, ControlExecOptions{
			Command:      commandBuilder(arts.ServiceStartPlan, target),
			PIDFile:      arts.ServicePID,
			ServiceURL:   serviceURL,
			ReadyURL:     readyURL,
			ReadyTimeout: readyTimeout,
			LogFile:      arts.ServiceLog,
			RepoDir:      repoDir,
			Profile:      profile,
		})
		responseFile := strings.TrimSuffix(requestFile, ".request") + ".response"
		respData, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if err := os.WriteFile(responseFile, respData, 0o644); err != nil {
			return err
		}
		_ = os.Remove(requestFile)
		if resp.Status == StatusOK {
			appendControlTimeline(serviceResult, &req, "completed", fmt.Sprintf("%s action completed", req.Action), nil)
		} else if resp.Error != nil {
			appendControlTimeline(serviceResult, &req, "failed", resp.Error.Message, map[string]string{"error_code": resp.Error.Code})
		}
		if resp.Status == StatusOK && req.Action == ActionRestart {
			var restartResult RestartActionResult
			if err := json.Unmarshal(resp.Result, &restartResult); err != nil {
				return err
			}
			*currentPID = restartResult.NewPID
			if err := writeServiceReady(arts, serviceName, target, serviceURL, readyURL, ProbeResult{
				StatusCode: restartResult.StatusCode,
				Headers:    restartResult.ResponseHeaders,
				Body:       restartResult.ResponseBody,
			}); err != nil {
				return err
			}
			_ = serviceResult.Append(withService(NewEvent(CodeServiceReady, LevelInfo, readyURL), serviceName, map[string]string{"ready_url": readyURL}))
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordExec, LevelInfo, "service readiness probe"), serviceName, nil))
			_ = verificationResult.Append(withService(NewEvent(CodeVerificationServiceReady, LevelInfo, readyURL), serviceName, map[string]string{"ready_url": readyURL}))
		}
	}
	return nil
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

func newEventLogFile(path string, out io.Writer) (*eventLogFile, error) {
	log := NewEventLog()
	if out == nil {
		out = io.Discard
	}
	f := &eventLogFile{path: path, log: log, out: out}
	return f, f.flush()
}

func (f *eventLogFile) Append(event Event) error {
	f.log.Events = append(f.log.Events, event)
	if err := f.flush(); err != nil {
		return err
	}
	if f.out != nil {
		_, _ = fmt.Fprintln(f.out, formatEventLogLine(event))
	}
	return nil
}

func (f *eventLogFile) flush() error {
	data, err := json.Marshal(f.log)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, append(data, '\n'), 0o644)
}

func formatEventLogLine(event Event) string {
	scope := event.Kind
	switch {
	case event.Service != "":
		scope = fmt.Sprintf("service %s", event.Service)
	case event.Repo != "":
		scope = fmt.Sprintf("repo %s", event.Repo)
	case scope == "":
		scope = "worker"
	}

	line := fmt.Sprintf("%s [%s] %s: %s", event.Time, event.Level, scope, event.Message)
	if len(event.Details) == 0 {
		return line
	}

	keys := make([]string, 0, len(event.Details))
	for key := range event.Details {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var detailParts []string
	for _, key := range keys {
		detailParts = append(detailParts, fmt.Sprintf("%s=%s", key, formatLogDetailValue(event.Details[key])))
	}
	return fmt.Sprintf("%s (%s)", line, strings.Join(detailParts, ", "))
}

func formatLogDetailValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " ,=()") {
		return strconv.Quote(value)
	}
	return value
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

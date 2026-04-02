package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	defer serviceResult.Close()
	mirrordResult, err := newEventLogFile(arts.MirrordResult, os.Stdout)
	if err != nil {
		return err
	}
	defer mirrordResult.Close()
	verificationResult, err := newEventLogFile(arts.VerificationResult, os.Stdout)
	if err != nil {
		return err
	}
	defer verificationResult.Close()
	bootstrapResult, err := newEventLogFile(arts.BootstrapResult, os.Stdout)
	if err != nil {
		return err
	}
	defer bootstrapResult.Close()

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

	if err := bootstrapPayloadRepos(payload, opts.WorkspaceDir, arts, repoSpecs, RunModeService, bootstrapResult); err != nil {
		return err
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
	appendLine(arts.MirrordPlan, fmt.Sprintf("mirrord exec --target %s -- dobby exec-plan --plan-file %s", target, arts.ServiceStartPlan))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStart, LevelInfo, "Dobby starts the service on "+serviceURL+". Dobby will not let master down!"), serviceName, map[string]string{"url": serviceURL, "target": target}))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStartStrategy, LevelInfo, plan.Strategy), serviceName, nil))
	_ = serviceResult.Append(withService(NewEvent(CodeServiceStartCommand, LevelInfo, plan.Description), serviceName, nil))

	commandBuilder := opts.PlanCommandBuilder
	if commandBuilder == nil {
		commandBuilder = func(planFile, target string) []string {
			return []string{"mirrord", "exec", "--target", target, "--", "dobby", "exec-plan", "--plan-file", planFile}
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
	controlLoop := serviceControlLoop{
		artifacts:          arts,
		serviceName:        serviceName,
		repoDir:            repoDir,
		profile:            runtimeProfile,
		serviceURL:         serviceURL,
		readyURL:           readyURL,
		readyTimeout:       readyTimeout,
		target:             target,
		commandBuilder:     commandBuilder,
		serviceResult:      serviceResult,
		mirrordResult:      mirrordResult,
		verificationResult: verificationResult,
	}
	for {
		select {
		case <-ctx.Done():
			_ = Terminate(currentPID, 2*time.Second)
			return ctx.Err()
		default:
		}
		restarted, err := controlLoop.processPendingRequests(ctx, &currentPID)
		if err != nil {
			return err
		}
		if restarted {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if ProcessStatus(currentPID) != StateRunning {
			_ = serviceResult.Append(withService(NewEvent(CodeServiceSessionFail, LevelError, fmt.Sprintf("Bad Dobby! Process %d has died, sir!", currentPID)), serviceName, map[string]string{"pid": fmt.Sprintf("%d", currentPID)}))
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
				_ = serviceResult.Append(withService(NewEvent(CodeServiceRecover, LevelWarn, "Dobby sees dirty iptables, sir. Dobby will try to fix it"), serviceName, nil))
				if recoverMirrordTarget(ctx, payload.Namespace, serviceName, target, arts.ServiceLog, serviceResult) {
					retriedRecovery = true
					continue
				}
				_ = serviceResult.Append(withService(NewEvent(CodeServiceRecoverFail, LevelError, "Dobby tried, sir, but the recovery has failed. Dobby will iron his hands later"), serviceName, nil))
			}
			_ = serviceResult.Append(withService(NewEvent(CodeServiceExitedBeforeReady, LevelError, "Dobby's service has died before it was ready, sir! *slams oven door on hands*"), serviceName, nil))
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "service process exited before readiness"), serviceName, nil))
			_ = verificationResult.Append(withService(NewEvent(CodeVerificationFail, LevelError, "service process exited before readiness"), serviceName, nil))
		} else {
			_ = serviceResult.Append(withService(NewEvent(CodeServiceReadyTimeout, LevelError, "Dobby waited and waited, but the service refuses to wake at "+readyURL+", sir"), serviceName, nil))
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "service readiness probe"), serviceName, nil))
			_ = verificationResult.Append(withService(NewEvent(CodeVerificationFail, LevelError, "service did not become ready"), serviceName, nil))
		}
		return nil, err
	}
}

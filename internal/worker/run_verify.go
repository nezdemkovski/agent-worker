package worker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runVerifyMode(ctx context.Context, payload *WorkerPayload, opts RunOptions) error {
	arts, err := initWorkerArtifacts(opts.ArtifactsDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.WorkspaceDir, 0o755); err != nil {
		return fmt.Errorf("mkdir workspace: %w", err)
	}

	bootstrapResult, err := newEventLogFile(arts.BootstrapResult)
	if err != nil {
		return err
	}
	serviceResult, err := newEventLogFile(arts.ServiceResult)
	if err != nil {
		return err
	}
	mirrordResult, err := newEventLogFile(arts.MirrordResult)
	if err != nil {
		return err
	}
	verificationResult, err := newEventLogFile(arts.VerificationResult)
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

	var hadFailure bool
	profile := strings.TrimSpace(payload.VerificationProfile)

	bootstrapMode := RunModeVerify
	if profile == "smoke" {
		bootstrapMode = RunModeSmoke
		if failed, err := runVerifyServiceProbes(ctx, payload, serviceSpecs, arts, serviceResult, mirrordResult); err != nil {
			return err
		} else if failed {
			hadFailure = true
		}
	}

	if err := bootstrapPayloadRepos(payload, opts.WorkspaceDir, arts, repoSpecs, bootstrapMode, bootstrapResult); err != nil {
		return err
	}

	if profile != "smoke" {
		if failed, err := runVerifyServiceProbes(ctx, payload, serviceSpecs, arts, serviceResult, mirrordResult); err != nil {
			return err
		} else if failed {
			hadFailure = true
		}
	}

	if failed, err := runVerificationProfileChecks(ctx, payload, repoSpecs, serviceSpecs, opts.WorkspaceDir, arts, verificationResult, mirrordResult, serviceResult); err != nil {
		return err
	} else if failed {
		hadFailure = true
	}

	if hadFailure {
		return fmt.Errorf("verification failed")
	}
	return nil
}

func bootstrapPayloadRepos(payload *WorkerPayload, workspaceDir string, arts workerArtifacts, repoSpecs map[string]RepoSpec, runMode RunMode, bootstrapResult *eventLogFile) error {
	for _, repo := range payload.Repos {
		spec := repoSpecs[repo]
		repoDir := spec.Path
		if strings.TrimSpace(repoDir) == "" {
			repoDir = filepath.Join(workspaceDir, repo)
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
			RunMode:      runMode,
			PNPMStoreDir: os.Getenv("PNPM_STORE_DIR"),
			PNPMStateDir: os.Getenv("PNPM_STATE_DIR"),
		})
		appendLines(arts.ClonePlan, result.ClonePlan)
		appendLines(arts.CheckoutResult, result.CheckoutResult)
		appendLines(arts.BranchResult, result.BranchResult)
		appendLines(arts.BootstrapPlan, result.BootstrapPlan)
		for _, line := range result.BootstrapResult {
			_ = bootstrapResult.Append(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, line), repo))
		}
		if err != nil {
			appendLine(arts.CheckoutResult, fmt.Sprintf("FAIL %s: %v", repo, err))
			return err
		}
	}
	return nil
}

func runVerifyServiceProbes(ctx context.Context, payload *WorkerPayload, serviceSpecs map[string]ServiceSpec, arts workerArtifacts, serviceResult, mirrordResult *eventLogFile) (bool, error) {
	if len(payload.Services) == 0 {
		_ = serviceResult.Append(NewEvent(CodeServiceSkip, LevelInfo, "no service interception requested"))
		_ = mirrordResult.Append(NewEvent(CodeMirrordSkip, LevelInfo, "no service interception requested"))
		return false, nil
	}

	var hadFailure bool
	for _, service := range payload.Services {
		if strings.TrimSpace(service) == "" {
			continue
		}
		spec := serviceSpecs[service]
		target := firstNonEmpty(spec.Target, "deploy/"+service)
		repoDir := firstNonEmpty(spec.Workdir, filepath.Join("/workspace", service))

		_ = serviceResult.Append(withService(NewEvent(CodeServiceTarget, LevelInfo, target), service, map[string]string{"target": target}))

		switch strings.TrimSpace(payload.VerificationProfile) {
		case "smoke":
			appendLine(arts.MirrordPlan, fmt.Sprintf("mirrord exec --target %s -- true", target))
			if !commandExists("mirrord") {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "mirrord unavailable"), service, nil))
				continue
			}
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSmokeProbe, LevelInfo, "smoke probe"), service, nil))
			lines, err := runCommandContext(ctx, "", nil, "mirrord", "exec", "--target", target, "--", "true")
			appendOutputEvents(mirrordResult, CodeMirrordExec, LevelInfo, service, "", lines)
			if err != nil {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelWarn, "mirrord smoke probe"), service, nil))
			}
		case "backend", "full":
			if !dirExists(repoDir) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "no matching repo checkout at "+repoDir), service, nil))
				continue
			}
			if !fileExists(filepath.Join(repoDir, "go.mod")) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelInfo, "backend repo not detected at "+repoDir), service, nil))
				continue
			}
			appendLine(arts.MirrordPlan, fmt.Sprintf("mirrord exec --target %s -- go test ./...", target))
			if !commandExists("mirrord") {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "mirrord unavailable"), service, nil))
				continue
			}
			if !commandExists("go") {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "go unavailable"), service, nil))
				continue
			}
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordGoTest, LevelInfo, "go test via mirrord"), service, nil))
			lines, err := runCommandContext(ctx, repoDir, nil, "mirrord", "exec", "--target", target, "--", "go", "test", "./...")
			appendOutputEvents(mirrordResult, CodeMirrordExec, LevelInfo, service, "", lines)
			if err != nil {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "mirrord go test ./..."), service, nil))
				hadFailure = true
			}
		case "frontend":
			if !dirExists(repoDir) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "no matching repo checkout at "+repoDir), service, nil))
				continue
			}
			if !fileExists(filepath.Join(repoDir, "package.json")) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelInfo, "frontend repo not detected at "+repoDir), service, nil))
				continue
			}
			appendLine(arts.MirrordPlan, fmt.Sprintf("mirrord exec --target %s -- pnpm test", target))
			if !commandExists("mirrord") {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "mirrord unavailable"), service, nil))
				continue
			}
			if !commandExists("pnpm") {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "pnpm unavailable"), service, nil))
				continue
			}
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordPnpmTest, LevelInfo, "pnpm test via mirrord"), service, nil))
			lines, err := runCommandContext(ctx, repoDir, nil, "mirrord", "exec", "--target", target, "--", "pnpm", "test")
			appendOutputEvents(mirrordResult, CodeMirrordExec, LevelInfo, service, "", lines)
			if err != nil {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, LevelError, "mirrord pnpm test"), service, nil))
				hadFailure = true
			}
		default:
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "mirrord execution not defined for profile "+payload.VerificationProfile), service, nil))
		}
	}

	return hadFailure, nil
}

func runVerificationProfileChecks(ctx context.Context, payload *WorkerPayload, repoSpecs map[string]RepoSpec, serviceSpecs map[string]ServiceSpec, workspaceDir string, arts workerArtifacts, verificationResult, mirrordResult, serviceResult *eventLogFile) (bool, error) {
	switch strings.TrimSpace(payload.VerificationProfile) {
	case "smoke":
		_ = verificationResult.Append(NewEvent(CodeVerificationSmoke, LevelInfo, "workspace bootstrap"))
		if hasNonWhitespaceFile(arts.WorkspaceManifest) {
			_ = verificationResult.Append(NewEvent(CodeVerificationOK, LevelInfo, "workspace manifest present"))
			return false, nil
		}
		_ = verificationResult.Append(NewEvent(CodeVerificationFail, LevelError, "workspace manifest missing"))
		return true, nil
	case "backend", "frontend", "full":
		var hadFailure bool
		for _, repo := range payload.Repos {
			spec := repoSpecs[repo]
			repoDir := spec.Path
			if strings.TrimSpace(repoDir) == "" {
				repoDir = filepath.Join(workspaceDir, repo)
			}
			if payload.VerificationProfile == "backend" || payload.VerificationProfile == "full" {
				if fileExists(filepath.Join(repoDir, "go.mod")) {
					if commandExists("go") {
						_ = verificationResult.Append(eventWithRepo(NewEvent(CodeVerificationGoTest, LevelInfo, "go test ./..."), repo))
						lines, err := runCommandContext(ctx, repoDir, nil, "go", "test", "./...")
						appendOutputEvents(verificationResult, CodeVerificationGoTest, LevelInfo, "", repo, lines)
						if err != nil {
							_ = verificationResult.Append(eventWithRepo(NewEvent(CodeVerificationFail, LevelError, "go test ./..."), repo))
							hadFailure = true
						}
					} else {
						_ = verificationResult.Append(eventWithRepo(NewEvent(CodeVerificationSkip, LevelWarn, "go unavailable"), repo))
					}
				}
			}
			if payload.VerificationProfile == "frontend" || payload.VerificationProfile == "full" {
				if fileExists(filepath.Join(repoDir, "package.json")) {
					if commandExists("pnpm") {
						_ = verificationResult.Append(eventWithRepo(NewEvent(CodeVerificationPlanPnpmTest, LevelInfo, "pnpm test (pending dependency bootstrap)"), repo))
					} else {
						_ = verificationResult.Append(eventWithRepo(NewEvent(CodeVerificationSkip, LevelWarn, "pnpm unavailable"), repo))
					}
				}
			}
		}
		return hadFailure, nil
	default:
		_ = verificationResult.Append(NewEvent(CodeVerificationSkip, LevelWarn, "unknown verification profile: "+payload.VerificationProfile))
		return false, nil
	}
}

func runCommandContext(ctx context.Context, dir string, env map[string]string, name string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = setEnvVar(cmd.Env, k, v)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return splitOutputLines(stdout.String(), stderr.String()), err
}

func appendOutputEvents(log *eventLogFile, code EventCode, level EventLevel, service, repo string, lines []string) {
	for _, line := range lines {
		event := NewEvent(code, level, line)
		if repo != "" {
			event.Repo = repo
		}
		if service != "" {
			event.Service = service
		}
		_ = log.Append(event)
	}
}

func hasNonWhitespaceFile(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(data)) != ""
}

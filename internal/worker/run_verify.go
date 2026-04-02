package worker

import (
	"context"
	"fmt"
	"os"
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

	bootstrapResult, err := newEventLogFile(arts.BootstrapResult, os.Stdout)
	if err != nil {
		return err
	}
	defer bootstrapResult.Close()
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
		appendLines(arts.CheckoutResult, result.CheckoutOutput)
		appendLines(arts.BranchResult, result.BranchOutput)
		appendLines(arts.BootstrapPlan, result.BootstrapPlan)
		appendBootstrapTimeline(bootstrapResult, repo, repoDir, result)
		if err != nil {
			appendLine(arts.CheckoutResult, fmt.Sprintf("FAIL %s: %v", repo, err))
			return err
		}
	}
	return nil
}

type verifyProbe struct {
	profiles  []string
	detector  string // "go.mod", "package.json", or "" for smoke
	tools     []string
	eventCode EventCode
	eventMsg  string
	failLevel EventLevel
	failMsg   string
	buildArgs func(target, repoDir string) []string
	planFmt   func(target string) string
}

var verifyProbes = []verifyProbe{
	{
		profiles:  []string{"smoke"},
		tools:     []string{"mirrord"},
		eventCode: CodeMirrordSmokeProbe,
		eventMsg:  "smoke probe",
		failLevel: LevelWarn,
		failMsg:   "mirrord smoke probe",
		buildArgs: func(target, _ string) []string { return []string{"mirrord", "exec", "--target", target, "--", "true"} },
		planFmt:   func(target string) string { return fmt.Sprintf("mirrord exec --target %s -- true", target) },
	},
	{
		profiles:  []string{"backend", "full"},
		detector:  "go.mod",
		tools:     []string{"mirrord", "go"},
		eventCode: CodeMirrordGoTest,
		eventMsg:  "go test via mirrord",
		failLevel: LevelError,
		failMsg:   "mirrord go test ./...",
		buildArgs: func(target, _ string) []string { return []string{"mirrord", "exec", "--target", target, "--", "go", "test", "./..."} },
		planFmt:   func(target string) string { return fmt.Sprintf("mirrord exec --target %s -- go test ./...", target) },
	},
	{
		profiles:  []string{"frontend"},
		detector:  "package.json",
		tools:     []string{"mirrord", "pnpm"},
		eventCode: CodeMirrordPnpmTest,
		eventMsg:  "pnpm test via mirrord",
		failLevel: LevelError,
		failMsg:   "mirrord pnpm test",
		buildArgs: func(target, _ string) []string { return []string{"mirrord", "exec", "--target", target, "--", "pnpm", "test"} },
		planFmt:   func(target string) string { return fmt.Sprintf("mirrord exec --target %s -- pnpm test", target) },
	},
}

func runVerifyServiceProbes(ctx context.Context, payload *WorkerPayload, serviceSpecs map[string]ServiceSpec, arts workerArtifacts, serviceResult, mirrordResult *eventLogFile) (bool, error) {
	if len(payload.Services) == 0 {
		_ = serviceResult.Append(NewEvent(CodeServiceSkip, LevelInfo, "no service interception requested"))
		_ = mirrordResult.Append(NewEvent(CodeMirrordSkip, LevelInfo, "no service interception requested"))
		return false, nil
	}

	profile := strings.TrimSpace(payload.VerificationProfile)
	var hadFailure bool
	for _, service := range payload.Services {
		if strings.TrimSpace(service) == "" {
			continue
		}
		spec := serviceSpecs[service]
		target := firstNonEmpty(spec.Target, "deploy/"+service)
		repoDir := firstNonEmpty(spec.Workdir, filepath.Join("/workspace", service))

		_ = serviceResult.Append(withService(NewEvent(CodeServiceTarget, LevelInfo, target), service, map[string]string{"target": target}))

		probe := findVerifyProbe(profile)
		if probe == nil {
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "mirrord execution not defined for profile "+profile), service, nil))
			continue
		}

		if probe.detector != "" {
			if !dirExists(repoDir) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, "no matching repo checkout at "+repoDir), service, nil))
				continue
			}
			if !fileExists(filepath.Join(repoDir, probe.detector)) {
				_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelInfo, probe.detector+" not detected at "+repoDir), service, nil))
				continue
			}
		}

		appendLine(arts.MirrordPlan, probe.planFmt(target))

		missingTool := ""
		for _, tool := range probe.tools {
			if !commandExists(tool) {
				missingTool = tool
				break
			}
		}
		if missingTool != "" {
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordSkip, LevelWarn, missingTool+" unavailable"), service, nil))
			continue
		}

		_ = mirrordResult.Append(withService(NewEvent(probe.eventCode, LevelInfo, probe.eventMsg), service, nil))
		args := probe.buildArgs(target, repoDir)
		lines, err := runLines(ctx, repoDir, nil, args[0], args[1:]...)
		appendOutputEvents(mirrordResult, CodeMirrordExec, LevelInfo, service, "", lines)
		if err != nil {
			_ = mirrordResult.Append(withService(NewEvent(CodeMirrordFail, probe.failLevel, probe.failMsg), service, nil))
			if probe.failLevel == LevelError {
				hadFailure = true
			}
		}
	}

	return hadFailure, nil
}

func findVerifyProbe(profile string) *verifyProbe {
	for i := range verifyProbes {
		for _, p := range verifyProbes[i].profiles {
			if p == profile {
				return &verifyProbes[i]
			}
		}
	}
	return nil
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
						lines, err := runLines(ctx, repoDir, nil, "go", "test", "./...")
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

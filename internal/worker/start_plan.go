package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type StartPlanOptions struct {
	Service        string
	WorkDir        string
	RuntimeProfile RuntimeProfile
	EntryPoint     string
	StartStrategy  string
	Port           string
}

func PlanStart(opts StartPlanOptions) (*TypedStartPlan, error) {
	if opts.WorkDir == "" {
		return nil, fmt.Errorf("no matching repo checkout at %s", opts.WorkDir)
	}
	if info, err := os.Stat(opts.WorkDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no matching repo checkout at %s", opts.WorkDir)
	}

	profile := opts.RuntimeProfile
	strategy := StartStrategy(strings.TrimSpace(opts.StartStrategy))
	if strategy == "" {
		switch profile {
		case ProfileGoHTTP:
			strategy = StrategyGoRun
		case ProfileNodeHTTP:
			strategy = StrategyNpmAuto
		}
	}

	switch profile {
	case ProfileGoHTTP:
		return planGoHTTP(opts, strategy)
	case ProfileNodeHTTP:
		return planNodeHTTP(opts, strategy)
	default:
		return nil, fmt.Errorf("unsupported runtime/start strategy %s:%s", profile, strategy)
	}
}

func planGoHTTP(opts StartPlanOptions, strategy StartStrategy) (*TypedStartPlan, error) {
	entrypoint := opts.EntryPoint
	if entrypoint == "" {
		entrypoint = fmt.Sprintf("./cmd/%s/main.go", opts.Service)
	}
	if !fileExists(filepath.Join(opts.WorkDir, strings.TrimPrefix(entrypoint, "./"))) && !fileExists(entrypoint) {
		return nil, fmt.Errorf("unsupported service entrypoint %s", entrypoint)
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("go unavailable")
	}

	checks := []PlanCheck{
		{Type: CheckFileExists, Path: filepath.Join(opts.WorkDir, strings.TrimPrefix(entrypoint, "./"))},
		{Type: CheckCommandExists, Name: "go"},
	}

	switch strategy {
	case StrategyAir:
		if _, err := exec.LookPath("air"); err != nil {
			return nil, fmt.Errorf("air unavailable")
		}
		checks = append(checks, PlanCheck{Type: CheckCommandExists, Name: "air"})
		return &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       string(StrategyAir),
			Workdir:        opts.WorkDir,
			Checks:         checks,
			Steps: []PlanStep{
				{Type: StepMkdirAll, Path: filepath.Join(opts.WorkDir, ".ndev-air"), Mode: 0o755},
				{Type: StepWriteFile, Path: filepath.Join(opts.WorkDir, ".ndev-air.toml"), Mode: 0o644, Content: airConfig(entrypoint, opts.Port)},
				{Type: StepRun, Command: "air", Args: []string{"-c", ".ndev-air.toml"}, Workdir: opts.WorkDir, Exec: true},
			},
			Description: "prepare Air config and start air",
		}, nil
	case StrategyGoRun, "":
		return &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       string(StrategyGoRun),
			Workdir:        opts.WorkDir,
			Checks:         checks,
			Steps: []PlanStep{
				{Type: StepRun, Command: "go", Args: []string{"run", entrypoint, "--port", opts.Port}, Workdir: opts.WorkDir, Exec: true},
			},
			Description: "run go service entrypoint",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime/start strategy %s:%s", opts.RuntimeProfile, strategy)
	}
}

func planNodeHTTP(opts StartPlanOptions, strategy StartStrategy) (*TypedStartPlan, error) {
	packageJSON := filepath.Join(opts.WorkDir, "package.json")
	if !fileExists(packageJSON) {
		return nil, fmt.Errorf("package.json not found at %s", opts.WorkDir)
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		return nil, fmt.Errorf("pnpm unavailable")
	}
	scripts, err := readPackageScripts(packageJSON)
	if err != nil {
		return nil, err
	}

	portEnv := map[string]string{"PORT": opts.Port}
	baseChecks := []PlanCheck{
		{Type: CheckFileExists, Path: packageJSON},
		{Type: CheckCommandExists, Name: "pnpm"},
		{Type: CheckDirExists, Path: filepath.Join(opts.WorkDir, "node_modules")},
	}

	switch strategy {
	case StrategyNpmAuto, "":
		if _, err := exec.LookPath("npm"); err != nil {
			return nil, fmt.Errorf("npm unavailable")
		}
		checks := append(baseChecks, PlanCheck{Type: CheckCommandExists, Name: "npm"})
		hasStart := scripts["start"]
		hasBuild := scripts["build"]
		hasDev := scripts["dev"]
		switch {
		case hasStart && hasBuild && hasDev:
			return &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       string(StrategyNpmAuto),
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Type: StepRun, Command: "npm", Args: []string{"run", "build"}, Workdir: opts.WorkDir, Env: portEnv},
					{Type: StepRun, Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Fallback: []PlanStep{
					{Type: StepRun, Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: "try npm run build and npm run start, fallback to npm run dev",
			}, nil
		case hasStart && hasBuild:
			return &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       string(StrategyNpmAuto),
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Type: StepRun, Command: "npm", Args: []string{"run", "build"}, Workdir: opts.WorkDir, Env: portEnv},
					{Type: StepRun, Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: "run npm build and npm start",
			}, nil
		case hasStart:
			return &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       string(StrategyNpmAuto),
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Type: StepRun, Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: "run npm start",
			}, nil
		case hasDev:
			return &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       string(StrategyNpmAuto),
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Type: StepRun, Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: "run npm dev",
			}, nil
		default:
			return nil, fmt.Errorf("package.json is missing both start and dev scripts")
		}
	case StrategyPnpmDev:
		checks := append(baseChecks[:2:2], PlanCheck{Type: CheckFileExists, Path: filepath.Join(opts.WorkDir, "node_modules/.bin/tsx")})
		return &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       string(StrategyPnpmDev),
			Workdir:        opts.WorkDir,
			Env:            portEnv,
			Checks:         checks,
			Steps: []PlanStep{
				{Type: StepRun, Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
			},
			Description: "run npm dev",
		}, nil
	case StrategyPnpmStart:
		return &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       string(StrategyPnpmStart),
			Workdir:        opts.WorkDir,
			Env:            portEnv,
			Checks:         baseChecks,
			Steps: []PlanStep{
				{Type: StepRun, Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
			},
			Description: "run npm start",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime/start strategy %s:%s", opts.RuntimeProfile, strategy)
	}
}

func readPackageScripts(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package.json: %w", err)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package.json: %w", err)
	}
	result := map[string]bool{}
	for name, script := range pkg.Scripts {
		if strings.TrimSpace(script) != "" {
			result[name] = true
		}
	}
	return result, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func airConfig(entrypoint, port string) string {
	return fmt.Sprintf(`root = "."
tmp_dir = ".ndev-air"

[build]
  cmd = "go build -o ./.ndev-air/service %s"
  bin = "./.ndev-air/service"
  entrypoint = ["./.ndev-air/service", "--port", %q]
  exclude_dir = ["assets", "tmp", "vendor", "testdata", ".git", "node_modules", ".ndev-air"]
  send_interrupt = true
  stop_on_error = true

[log]
  main_only = true
`, entrypoint, port)
}

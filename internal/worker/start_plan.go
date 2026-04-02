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

type StartPlan struct {
	RuntimeProfile   RuntimeProfile
	ResolvedStrategy string
	StartCommand     string
	StartDescription string
	Plan             *TypedStartPlan
}

func PlanStart(opts StartPlanOptions) (StartPlan, error) {
	if opts.WorkDir == "" {
		return StartPlan{}, fmt.Errorf("no matching repo checkout at %s", opts.WorkDir)
	}
	if info, err := os.Stat(opts.WorkDir); err != nil || !info.IsDir() {
		return StartPlan{}, fmt.Errorf("no matching repo checkout at %s", opts.WorkDir)
	}

	profile := opts.RuntimeProfile
	strategy := strings.TrimSpace(opts.StartStrategy)
	if strategy == "" {
		switch profile {
		case ProfileGoHTTP:
			strategy = "go-run"
		case ProfileNodeHTTP:
			strategy = "npm-auto"
		}
	}

	switch profile {
	case ProfileGoHTTP:
		return planGoHTTP(opts, strategy)
	case ProfileNodeHTTP:
		return planNodeHTTP(opts, strategy)
	default:
		return StartPlan{}, fmt.Errorf("unsupported runtime/start strategy %s:%s", profile, strategy)
	}
}

func planGoHTTP(opts StartPlanOptions, strategy string) (StartPlan, error) {
	entrypoint := opts.EntryPoint
	if entrypoint == "" {
		entrypoint = fmt.Sprintf("./cmd/%s/main.go", opts.Service)
	}
	if !fileExists(filepath.Join(opts.WorkDir, strings.TrimPrefix(entrypoint, "./"))) && !fileExists(entrypoint) {
		return StartPlan{}, fmt.Errorf("unsupported service entrypoint %s", entrypoint)
	}
	if _, err := exec.LookPath("go"); err != nil {
		return StartPlan{}, fmt.Errorf("go unavailable")
	}

	plan := StartPlan{RuntimeProfile: opts.RuntimeProfile, ResolvedStrategy: strategy}

	checks := []PlanCheck{
		{Type: "file_exists", Path: filepath.Join(opts.WorkDir, strings.TrimPrefix(entrypoint, "./"))},
		{Type: "command_exists", Name: "go"},
	}

	switch strategy {
	case "air":
		if _, err := exec.LookPath("air"); err != nil {
			return StartPlan{}, fmt.Errorf("air unavailable")
		}
		checks = append(checks, PlanCheck{Type: "command_exists", Name: "air"})
		plan.StartCommand = fmt.Sprintf(`set -eu
cd %s
mkdir -p .ndev-air
cat > .ndev-air.toml <<EOF
root = "."
tmp_dir = ".ndev-air"

[build]
  cmd = "go build -o ./.ndev-air/service %s"
  bin = "./.ndev-air/service"
  entrypoint = ["./.ndev-air/service", "--port", %s]
  exclude_dir = ["assets", "tmp", "vendor", "testdata", ".git", "node_modules", ".ndev-air"]
  send_interrupt = true
  stop_on_error = true

[log]
  main_only = true
EOF
exec air -c .ndev-air.toml`, shellQuote(opts.WorkDir), entrypoint, shellQuote(opts.Port))
		plan.StartDescription = fmt.Sprintf("cd %s && write .ndev-air.toml && air -c .ndev-air.toml", opts.WorkDir)
		plan.Plan = &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       "air",
			Workdir:        opts.WorkDir,
			Checks:         checks,
			Steps: []PlanStep{
				{Command: "air", Args: []string{"-c", ".ndev-air.toml"}, Workdir: opts.WorkDir, Exec: true},
			},
			Description: plan.StartDescription,
		}
	case "go-run", "":
		plan.ResolvedStrategy = "go-run"
		plan.StartCommand = fmt.Sprintf("set -eu && cd %s && exec go run %s --port %s", shellQuote(opts.WorkDir), shellQuote(entrypoint), shellQuote(opts.Port))
		plan.StartDescription = fmt.Sprintf("cd %s && go run %s --port %s", opts.WorkDir, entrypoint, opts.Port)
		plan.Plan = &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       "go-run",
			Workdir:        opts.WorkDir,
			Checks:         checks,
			Steps: []PlanStep{
				{Command: "go", Args: []string{"run", entrypoint, "--port", opts.Port}, Workdir: opts.WorkDir, Exec: true},
			},
			Description: plan.StartDescription,
		}
	default:
		return StartPlan{}, fmt.Errorf("unsupported runtime/start strategy %s:%s", opts.RuntimeProfile, strategy)
	}
	return plan, nil
}

func planNodeHTTP(opts StartPlanOptions, strategy string) (StartPlan, error) {
	packageJSON := filepath.Join(opts.WorkDir, "package.json")
	if !fileExists(packageJSON) {
		return StartPlan{}, fmt.Errorf("package.json not found at %s", opts.WorkDir)
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		return StartPlan{}, fmt.Errorf("pnpm unavailable")
	}
	scripts, err := readPackageScripts(packageJSON)
	if err != nil {
		return StartPlan{}, err
	}

	portEnv := map[string]string{"PORT": opts.Port}
	baseChecks := []PlanCheck{
		{Type: "file_exists", Path: packageJSON},
		{Type: "command_exists", Name: "pnpm"},
		{Type: "dir_exists", Path: filepath.Join(opts.WorkDir, "node_modules")},
	}

	plan := StartPlan{RuntimeProfile: opts.RuntimeProfile, ResolvedStrategy: strategy}
	switch strategy {
	case "npm-auto", "":
		plan.ResolvedStrategy = "npm-auto"
		if _, err := exec.LookPath("npm"); err != nil {
			return StartPlan{}, fmt.Errorf("npm unavailable")
		}
		checks := append(baseChecks, PlanCheck{Type: "command_exists", Name: "npm"})
		hasStart := scripts["start"]
		hasBuild := scripts["build"]
		hasDev := scripts["dev"]
		switch {
		case hasStart && hasBuild && hasDev:
			plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -d node_modules ]; then echo \"missing node_modules after bootstrap\" >&2; exit 1; fi && export PORT=%s && if npm run build; then exec npm run start; else echo \"[ndev] npm-auto build failed, falling back to npm run dev\" >&2; exec npm run dev; fi", shellQuote(opts.WorkDir), shellQuote(opts.Port))
			plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s try npm run build && npm run start, fallback to npm run dev", opts.WorkDir, opts.Port)
			plan.Plan = &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       "npm-auto",
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Command: "npm", Args: []string{"run", "build"}, Workdir: opts.WorkDir, Env: portEnv},
					{Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Fallback: []PlanStep{
					{Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: plan.StartDescription,
			}
		case hasStart && hasBuild:
			plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -d node_modules ]; then echo \"missing node_modules after bootstrap\" >&2; exit 1; fi && export PORT=%s && npm run build && exec npm run start", shellQuote(opts.WorkDir), shellQuote(opts.Port))
			plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s npm run build && npm run start", opts.WorkDir, opts.Port)
			plan.Plan = &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       "npm-auto",
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Command: "npm", Args: []string{"run", "build"}, Workdir: opts.WorkDir, Env: portEnv},
					{Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: plan.StartDescription,
			}
		case hasStart:
			plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -d node_modules ]; then echo \"missing node_modules after bootstrap\" >&2; exit 1; fi && export PORT=%s && exec npm run start", shellQuote(opts.WorkDir), shellQuote(opts.Port))
			plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s npm run start", opts.WorkDir, opts.Port)
			plan.Plan = &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       "npm-auto",
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: plan.StartDescription,
			}
		case hasDev:
			plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -d node_modules ]; then echo \"missing node_modules after bootstrap\" >&2; exit 1; fi && export PORT=%s && exec npm run dev", shellQuote(opts.WorkDir), shellQuote(opts.Port))
			plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s npm run dev", opts.WorkDir, opts.Port)
			plan.Plan = &TypedStartPlan{
				RuntimeProfile: string(opts.RuntimeProfile),
				Strategy:       "npm-auto",
				Workdir:        opts.WorkDir,
				Env:            portEnv,
				Checks:         checks,
				Steps: []PlanStep{
					{Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
				},
				Description: plan.StartDescription,
			}
		default:
			return StartPlan{}, fmt.Errorf("package.json is missing both start and dev scripts")
		}
	case "pnpm-dev":
		checks := append(baseChecks[:2:2], PlanCheck{Type: "file_exists", Path: filepath.Join(opts.WorkDir, "node_modules/.bin/tsx")})
		plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -x node_modules/.bin/tsx ]; then echo \"missing node_modules/.bin/tsx after bootstrap\" >&2; exit 1; fi && export PORT=%s && exec npm run dev", shellQuote(opts.WorkDir), shellQuote(opts.Port))
		plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s npm run dev", opts.WorkDir, opts.Port)
		plan.Plan = &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       "pnpm-dev",
			Workdir:        opts.WorkDir,
			Env:            portEnv,
			Checks:         checks,
			Steps: []PlanStep{
				{Command: "npm", Args: []string{"run", "dev"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
			},
			Description: plan.StartDescription,
		}
	case "pnpm-start":
		plan.StartCommand = fmt.Sprintf("set -eu && cd %s && if [ ! -d node_modules ]; then echo \"missing node_modules after bootstrap\" >&2; exit 1; fi && export PORT=%s && exec npm run start", shellQuote(opts.WorkDir), shellQuote(opts.Port))
		plan.StartDescription = fmt.Sprintf("cd %s && PORT=%s npm run start", opts.WorkDir, opts.Port)
		plan.Plan = &TypedStartPlan{
			RuntimeProfile: string(opts.RuntimeProfile),
			Strategy:       "pnpm-start",
			Workdir:        opts.WorkDir,
			Env:            portEnv,
			Checks:         baseChecks,
			Steps: []PlanStep{
				{Command: "npm", Args: []string{"run", "start"}, Workdir: opts.WorkDir, Env: portEnv, Exec: true},
			},
			Description: plan.StartDescription,
		}
	default:
		return StartPlan{}, fmt.Errorf("unsupported runtime/start strategy %s:%s", opts.RuntimeProfile, strategy)
	}
	return plan, nil
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

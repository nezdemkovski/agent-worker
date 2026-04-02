package worker

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RunMode string

const (
	RunModeService RunMode = "service"
	RunModeVerify  RunMode = "verify"
	RunModeSmoke   RunMode = "smoke"
)

type BootstrapRepoOptions struct {
	Repo         string
	RepoDir      string
	RemoteURL    string
	Branch       string
	RunMode      RunMode
	PNPMStoreDir string
	PNPMStateDir string
}

type BootstrapRepoResult struct {
	ClonePlan       []string `json:"clone_plan,omitempty"`
	CheckoutOutput  []string `json:"checkout_output,omitempty"`
	CheckoutEvents  []Event  `json:"checkout_events,omitempty"`
	BranchOutput    []string `json:"branch_output,omitempty"`
	BranchEvents    []Event  `json:"branch_events,omitempty"`
	BootstrapPlan   []string `json:"bootstrap_plan,omitempty"`
	BootstrapOutput []string `json:"bootstrap_output,omitempty"`
	BootstrapEvents []Event  `json:"bootstrap_events,omitempty"`
	BranchReady     string   `json:"branch_ready,omitempty"`
	PreparedRepo    string   `json:"prepared_repo,omitempty"`
}

func BootstrapRepo(opts BootstrapRepoOptions) (*BootstrapRepoResult, error) {
	result := &BootstrapRepoResult{PreparedRepo: opts.Repo}

	if opts.RepoDir == "" {
		return result, fmt.Errorf("missing repo dir")
	}
	if opts.RemoteURL == "" {
		return result, fmt.Errorf("missing remote url")
	}
	if err := os.MkdirAll(opts.RepoDir, 0o755); err != nil {
		return result, fmt.Errorf("mkdir repo dir: %w", err)
	}

	shallow := opts.RunMode == RunModeService || opts.RunMode == RunModeSmoke
	clonePlan := fmt.Sprintf("git clone %s %s", opts.RemoteURL, opts.RepoDir)
	if shallow {
		clonePlan = fmt.Sprintf("git clone --depth 1 %s %s", opts.RemoteURL, opts.RepoDir)
	}
	result.ClonePlan = append(result.ClonePlan, clonePlan)
	result.ClonePlan = append(result.ClonePlan, fmt.Sprintf("git -C %s fetch --all --prune", opts.RepoDir))

	if !commandExists("git") {
		result.addCheckoutWarning(opts.Repo, "git unavailable in worker image")
		result.addBranchWarning(opts.Repo, "branch setup unavailable")
		return result, nil
	}

	gitDir := filepath.Join(opts.RepoDir, ".git")
	if dirExists(gitDir) {
		result.addCheckoutEvent(eventWithRepo(NewEvent(CodeRepoCheckout, LevelInfo, "fetching latest changes"), opts.Repo))
		out, err := runCommand(opts.RepoDir, "git", "-C", opts.RepoDir, "fetch", "--all", "--prune")
		result.addCheckoutOutput(opts.Repo, CodeRepoCheckout, out)
		if err != nil {
			result.addCheckoutFailure(opts.Repo, "git fetch --all --prune")
			return result, err
		}
	} else {
		_ = os.Remove(opts.RepoDir)
		result.addCheckoutEvent(eventWithRepo(NewEvent(CodeRepoClone, LevelInfo, "cloning repository"), opts.Repo))
		args := []string{"clone"}
		failLabel := "git clone"
		if shallow {
			args = append(args, "--depth", "1")
			failLabel = "git clone --depth 1"
		}
		args = append(args, opts.RemoteURL, opts.RepoDir)
		out, err := runCommand("", "git", args...)
		result.addCheckoutOutput(opts.Repo, CodeRepoClone, out)
		if err != nil {
			result.addCheckoutFailure(opts.Repo, failLabel)
			return result, err
		}
	}

	if opts.Branch != "" && dirExists(gitDir) {
		event := eventWithRepo(NewEvent(CodeRepoBranch, LevelInfo, "checking out branch "+opts.Branch), opts.Repo)
		event.Details = map[string]string{"branch": opts.Branch}
		result.addBranchEvent(event)
		out, err := runCommand(opts.RepoDir, "git", "-C", opts.RepoDir, "checkout", "-B", opts.Branch)
		result.addBranchOutput(opts.Repo, CodeRepoBranch, out)
		if err != nil {
			result.addBranchFailure(opts.Repo, fmt.Sprintf("git checkout -B %s", opts.Branch))
			return result, err
		}
		result.BranchReady = fmt.Sprintf("%s:%s", opts.Repo, opts.Branch)
	} else {
		result.addBranchWarning(opts.Repo, "branch setup unavailable")
	}

	if fileExists(filepath.Join(opts.RepoDir, "go.mod")) {
		if opts.RunMode == RunModeService {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("defer go module bootstrap for service startup: %s", opts.RepoDir))
			result.addBootstrapWarning(opts.Repo, "defer go module bootstrap to service startup")
		} else if opts.RunMode == RunModeSmoke {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip go module bootstrap for smoke verification: %s", opts.RepoDir))
			result.addBootstrapWarning(opts.Repo, "smoke verification skips go module bootstrap")
		} else {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("go -C %s mod download", opts.RepoDir))
			if commandExists("go") {
				result.addBootstrapEvent(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "downloading Go modules"), opts.Repo))
				out, err := runCommand(opts.RepoDir, "go", "mod", "download")
				result.addBootstrapOutput(opts.Repo, out)
				if err != nil {
					result.addBootstrapFailure(opts.Repo, "go mod download")
				}
			} else {
				result.addBootstrapWarning(opts.Repo, "go unavailable")
			}
		}
	}

	if fileExists(filepath.Join(opts.RepoDir, "pnpm-lock.yaml")) {
		if opts.RunMode == RunModeService {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s install --ignore-scripts", pnpmStoreDir(opts.PNPMStoreDir), pnpmStateDir(opts.PNPMStateDir), opts.RepoDir))
		} else if opts.RunMode == RunModeSmoke {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip pnpm bootstrap for smoke verification: %s", opts.RepoDir))
		} else {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s fetch", pnpmStoreDir(opts.PNPMStoreDir), pnpmStateDir(opts.PNPMStateDir), opts.RepoDir))
		}
		if commandExists("pnpm") {
			_ = cleanupPNPMProjectLinks(pnpmStoreDir(opts.PNPMStoreDir))
			if opts.RunMode == RunModeService {
				result.addBootstrapEvent(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "installing pnpm dependencies"), opts.Repo))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "install", "--ignore-scripts")
				result.addBootstrapOutput(opts.Repo, out)
				if err != nil {
					result.addBootstrapFailure(opts.Repo, "pnpm install --ignore-scripts")
				}
			} else if opts.RunMode == RunModeSmoke {
				result.addBootstrapWarning(opts.Repo, "smoke verification skips pnpm bootstrap")
			} else {
				result.addBootstrapEvent(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "fetching pnpm dependencies"), opts.Repo))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "fetch")
				result.addBootstrapOutput(opts.Repo, out)
				if err != nil {
					result.addBootstrapFailure(opts.Repo, "pnpm fetch")
				}
			}
		} else {
			result.addBootstrapWarning(opts.Repo, "pnpm unavailable")
		}
	} else if fileExists(filepath.Join(opts.RepoDir, "package.json")) {
		if opts.RunMode == RunModeSmoke {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip pnpm bootstrap for smoke verification: %s", opts.RepoDir))
			result.addBootstrapWarning(opts.Repo, "smoke verification skips pnpm bootstrap")
		} else {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s install --ignore-scripts", pnpmStoreDir(opts.PNPMStoreDir), pnpmStateDir(opts.PNPMStateDir), opts.RepoDir))
			if commandExists("pnpm") {
				result.addBootstrapEvent(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "installing pnpm dependencies"), opts.Repo))
				_ = cleanupPNPMProjectLinks(pnpmStoreDir(opts.PNPMStoreDir))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "install", "--ignore-scripts")
				result.addBootstrapOutput(opts.Repo, out)
				if err != nil {
					result.addBootstrapFailure(opts.Repo, "pnpm install --ignore-scripts")
				}
			} else {
				result.addBootstrapWarning(opts.Repo, "pnpm unavailable")
			}
		}
	}

	return result, nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func runCommand(dir, name string, args ...string) ([]string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	lines := splitOutputLines(stdout.String(), stderr.String())
	if err != nil {
		return lines, err
	}
	return lines, nil
}

func splitOutputLines(parts ...string) []string {
	var lines []string
	for _, part := range parts {
		part = strings.ReplaceAll(part, "\r\n", "\n")
		part = strings.TrimRight(part, "\n")
		if part == "" {
			continue
		}
		lines = append(lines, strings.Split(part, "\n")...)
	}
	return lines
}

func cleanupPNPMProjectLinks(storeRoot string) error {
	if storeRoot == "" {
		return nil
	}
	if !dirExists(storeRoot) {
		return nil
	}
	return filepath.WalkDir(storeRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "projects" {
			entries, readErr := os.ReadDir(path)
			if readErr != nil {
				return nil
			}
			for _, entry := range entries {
				_ = os.RemoveAll(filepath.Join(path, entry.Name()))
			}
		}
		return nil
	})
}

func pnpmStoreDir(dir string) string {
	if dir != "" {
		return dir
	}
	return "/cache/pnpm/store"
}

func pnpmStateDir(dir string) string {
	if dir != "" {
		return dir
	}
	return "/cache/pnpm/state"
}

func (r *BootstrapRepoResult) addCheckoutEvent(event Event) {
	r.CheckoutEvents = append(r.CheckoutEvents, event)
}

func (r *BootstrapRepoResult) addCheckoutOutput(repo string, code EventCode, lines []string) {
	r.CheckoutOutput = append(r.CheckoutOutput, lines...)
	r.CheckoutEvents = append(r.CheckoutEvents, repoOutputEvents(repo, code, lines)...)
}

func (r *BootstrapRepoResult) addCheckoutWarning(repo, message string) {
	r.CheckoutOutput = append(r.CheckoutOutput, "skipped: "+message)
	r.CheckoutEvents = append(r.CheckoutEvents, eventWithRepo(NewEvent(CodeRepoCheckout, LevelWarn, message), repo))
}

func (r *BootstrapRepoResult) addCheckoutFailure(repo, command string) {
	message := command + " failed"
	r.CheckoutOutput = append(r.CheckoutOutput, "failed: "+command)
	r.CheckoutEvents = append(r.CheckoutEvents, eventWithRepo(NewEvent(CodeRepoFail, LevelError, message), repo))
}

func (r *BootstrapRepoResult) addBranchEvent(event Event) {
	r.BranchEvents = append(r.BranchEvents, event)
}

func (r *BootstrapRepoResult) addBranchOutput(repo string, code EventCode, lines []string) {
	r.BranchOutput = append(r.BranchOutput, lines...)
	r.BranchEvents = append(r.BranchEvents, repoOutputEvents(repo, code, lines)...)
}

func (r *BootstrapRepoResult) addBranchWarning(repo, message string) {
	r.BranchOutput = append(r.BranchOutput, "skipped: "+message)
	r.BranchEvents = append(r.BranchEvents, eventWithRepo(NewEvent(CodeRepoBranch, LevelWarn, message), repo))
}

func (r *BootstrapRepoResult) addBranchFailure(repo, command string) {
	message := command + " failed"
	r.BranchOutput = append(r.BranchOutput, "failed: "+command)
	r.BranchEvents = append(r.BranchEvents, eventWithRepo(NewEvent(CodeRepoFail, LevelError, message), repo))
}

func (r *BootstrapRepoResult) addBootstrapEvent(event Event) {
	r.BootstrapEvents = append(r.BootstrapEvents, event)
}

func (r *BootstrapRepoResult) addBootstrapOutput(repo string, lines []string) {
	r.BootstrapOutput = append(r.BootstrapOutput, lines...)
	r.BootstrapEvents = append(r.BootstrapEvents, repoOutputEvents(repo, CodeRepoBootstrap, lines)...)
}

func (r *BootstrapRepoResult) addBootstrapWarning(repo, message string) {
	r.BootstrapOutput = append(r.BootstrapOutput, "skipped: "+message)
	r.BootstrapEvents = append(r.BootstrapEvents, eventWithRepo(NewEvent(CodeRepoBootstrap, LevelWarn, message), repo))
}

func (r *BootstrapRepoResult) addBootstrapFailure(repo, command string) {
	message := command + " failed"
	r.BootstrapOutput = append(r.BootstrapOutput, "failed: "+command)
	r.BootstrapEvents = append(r.BootstrapEvents, eventWithRepo(NewEvent(CodeRepoFail, LevelError, message), repo))
}

func repoOutputEvents(repo string, code EventCode, lines []string) []Event {
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		events = append(events, eventWithRepo(NewEvent(code, LevelInfo, line), repo))
	}
	return events
}

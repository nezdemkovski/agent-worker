package worker

import (
	"context"
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

	buildClonePlan(result, opts)

	if !commandExists("git") {
		result.checkout().warn(opts.Repo, "git unavailable in worker image")
		result.branch().warn(opts.Repo, "branch setup unavailable")
		return result, nil
	}

	if err := cloneOrFetch(result, opts); err != nil {
		return result, err
	}
	if err := checkoutBranch(result, opts); err != nil {
		return result, err
	}

	bootstrapGo(result, opts)
	bootstrapPNPM(result, opts)

	return result, nil
}

func buildClonePlan(result *BootstrapRepoResult, opts BootstrapRepoOptions) {
	shallow := opts.RunMode == RunModeService || opts.RunMode == RunModeSmoke
	clonePlan := fmt.Sprintf("git clone %s %s", opts.RemoteURL, opts.RepoDir)
	if shallow {
		clonePlan = fmt.Sprintf("git clone --depth 1 %s %s", opts.RemoteURL, opts.RepoDir)
	}
	result.ClonePlan = append(result.ClonePlan, clonePlan)
	result.ClonePlan = append(result.ClonePlan, fmt.Sprintf("git -C %s fetch --all --prune", opts.RepoDir))
}

func cloneOrFetch(result *BootstrapRepoResult, opts BootstrapRepoOptions) error {
	gitDir := filepath.Join(opts.RepoDir, ".git")
	shallow := opts.RunMode == RunModeService || opts.RunMode == RunModeSmoke

	co := result.checkout()
	if dirExists(gitDir) {
		co.event(eventWithRepo(NewEvent(CodeRepoCheckout, LevelInfo, "Dobby must fetch the latest changes, sir"), opts.Repo))
		out, err := runLines(context.Background(), opts.RepoDir, nil, "git", "-C", opts.RepoDir, "fetch", "--all", "--prune")
		co.output(opts.Repo, CodeRepoCheckout, out)
		if err != nil {
			co.fail(opts.Repo, "git fetch --all --prune")
			return err
		}
		return nil
	}

	_ = os.Remove(opts.RepoDir)
	co.event(eventWithRepo(NewEvent(CodeRepoClone, LevelInfo, "Dobby is honored to clone the repository, sir"), opts.Repo))
	args := []string{"clone"}
	failLabel := "git clone"
	if shallow {
		args = append(args, "--depth", "1")
		failLabel = "git clone --depth 1"
	}
	args = append(args, opts.RemoteURL, opts.RepoDir)
	out, err := runLines(context.Background(), "", nil, "git", args...)
	co.output(opts.Repo, CodeRepoClone, out)
	if err != nil {
		co.fail(opts.Repo, failLabel)
		return err
	}
	return nil
}

func checkoutBranch(result *BootstrapRepoResult, opts BootstrapRepoOptions) error {
	gitDir := filepath.Join(opts.RepoDir, ".git")
	br := result.branch()
	if opts.Branch == "" || !dirExists(gitDir) {
		br.warn(opts.Repo, "branch setup unavailable")
		return nil
	}
	event := eventWithRepo(NewEvent(CodeRepoBranch, LevelInfo, "Dobby switches to branch "+opts.Branch+", sir"), opts.Repo)
	event.Details = map[string]string{"branch": opts.Branch}
	br.event(event)
	out, err := runLines(context.Background(), opts.RepoDir, nil, "git", "-C", opts.RepoDir, "checkout", "-B", opts.Branch)
	br.output(opts.Repo, CodeRepoBranch, out)
	if err != nil {
		br.fail(opts.Repo, fmt.Sprintf("git checkout -B %s", opts.Branch))
		return err
	}
	result.BranchReady = fmt.Sprintf("%s:%s", opts.Repo, opts.Branch)
	return nil
}

func bootstrapGo(result *BootstrapRepoResult, opts BootstrapRepoOptions) {
	if !fileExists(filepath.Join(opts.RepoDir, "go.mod")) {
		return
	}
	bs := result.bootstrap()
	switch opts.RunMode {
	case RunModeService:
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("defer go module bootstrap for service startup: %s", opts.RepoDir))
		bs.warn(opts.Repo, "defer go module bootstrap to service startup")
	case RunModeSmoke:
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip go module bootstrap for smoke verification: %s", opts.RepoDir))
		bs.warn(opts.Repo, "smoke verification skips go module bootstrap")
	default:
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("go -C %s mod download", opts.RepoDir))
		if !commandExists("go") {
			bs.warn(opts.Repo, "go unavailable")
			return
		}
		bs.event(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "Dobby fetches the Go modules. Dobby is a good elf!"), opts.Repo))
		out, err := runLines(context.Background(), opts.RepoDir, nil, "go", "mod", "download")
		bs.output(opts.Repo, CodeRepoBootstrap, out)
		if err != nil {
			bs.fail(opts.Repo, "go mod download")
		}
	}
}

func bootstrapPNPM(result *BootstrapRepoResult, opts BootstrapRepoOptions) {
	hasLockfile := fileExists(filepath.Join(opts.RepoDir, "pnpm-lock.yaml"))
	hasPackageJSON := fileExists(filepath.Join(opts.RepoDir, "package.json"))
	if !hasLockfile && !hasPackageJSON {
		return
	}

	storeDir := pnpmStoreDir(opts.PNPMStoreDir)
	stateDir := pnpmStateDir(opts.PNPMStateDir)

	bs := result.bootstrap()

	if opts.RunMode == RunModeSmoke {
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip pnpm bootstrap for smoke verification: %s", opts.RepoDir))
		if commandExists("pnpm") && hasLockfile {
			_ = cleanupPNPMProjectLinks(storeDir)
		}
		bs.warn(opts.Repo, "smoke verification skips pnpm bootstrap")
		return
	}

	// With a lockfile in verify mode, use "fetch" (download only); otherwise "install --ignore-scripts".
	useFetch := hasLockfile && opts.RunMode == RunModeVerify
	if useFetch {
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s fetch", storeDir, stateDir, opts.RepoDir))
	} else {
		result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s install --ignore-scripts", storeDir, stateDir, opts.RepoDir))
	}

	if !commandExists("pnpm") {
		bs.warn(opts.Repo, "pnpm unavailable")
		return
	}
	_ = cleanupPNPMProjectLinks(storeDir)

	args := []string{"--store-dir", storeDir, "--config.state-dir=" + stateDir, "--dir", opts.RepoDir}
	if useFetch {
		bs.event(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "Dobby fetches pnpm dependencies, sir. Dobby does not complain"), opts.Repo))
		args = append(args, "fetch")
	} else {
		bs.event(eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "Dobby installs pnpm dependencies. Dobby is used to hard work, sir"), opts.Repo))
		args = append(args, "install", "--ignore-scripts")
	}
	out, err := runLines(context.Background(), "", nil, "pnpm", args...)
	bs.output(opts.Repo, CodeRepoBootstrap, out)
	if err != nil {
		if useFetch {
			bs.fail(opts.Repo, "pnpm fetch")
		} else {
			bs.fail(opts.Repo, "pnpm install --ignore-scripts")
		}
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

type bootstrapPhase struct {
	events *[]Event
	lines  *[]string
	code   EventCode
}

func (r *BootstrapRepoResult) checkout() bootstrapPhase {
	return bootstrapPhase{&r.CheckoutEvents, &r.CheckoutOutput, CodeRepoCheckout}
}

func (r *BootstrapRepoResult) branch() bootstrapPhase {
	return bootstrapPhase{&r.BranchEvents, &r.BranchOutput, CodeRepoBranch}
}

func (r *BootstrapRepoResult) bootstrap() bootstrapPhase {
	return bootstrapPhase{&r.BootstrapEvents, &r.BootstrapOutput, CodeRepoBootstrap}
}

func (p bootstrapPhase) event(e Event) {
	*p.events = append(*p.events, e)
}

func (p bootstrapPhase) output(repo string, code EventCode, out []string) {
	*p.lines = append(*p.lines, out...)
	for _, line := range out {
		*p.events = append(*p.events, eventWithRepo(NewEvent(code, LevelInfo, line), repo))
	}
}

func (p bootstrapPhase) warn(repo, message string) {
	*p.lines = append(*p.lines, "skipped: "+message)
	*p.events = append(*p.events, eventWithRepo(NewEvent(p.code, LevelWarn, message), repo))
}

func (p bootstrapPhase) fail(repo, command string) {
	*p.lines = append(*p.lines, "failed: "+command)
	*p.events = append(*p.events, eventWithRepo(NewEvent(CodeRepoFail, LevelError, command+" failed"), repo))
}

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
	CheckoutResult  []string `json:"checkout_result,omitempty"`
	BranchResult    []string `json:"branch_result,omitempty"`
	BootstrapPlan   []string `json:"bootstrap_plan,omitempty"`
	BootstrapResult []string `json:"bootstrap_result,omitempty"`
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
		result.CheckoutResult = append(result.CheckoutResult, fmt.Sprintf("SKIP %s: git unavailable in worker image", opts.Repo))
		result.BranchResult = append(result.BranchResult, fmt.Sprintf("SKIP %s: branch setup unavailable", opts.Repo))
		return result, nil
	}

	gitDir := filepath.Join(opts.RepoDir, ".git")
	if dirExists(gitDir) {
		result.CheckoutResult = append(result.CheckoutResult, fmt.Sprintf("FETCH %s", opts.Repo))
		out, err := runCommand(opts.RepoDir, "git", "-C", opts.RepoDir, "fetch", "--all", "--prune")
		result.CheckoutResult = append(result.CheckoutResult, out...)
		if err != nil {
			result.CheckoutResult = append(result.CheckoutResult, fmt.Sprintf("FAIL %s: git fetch --all --prune", opts.Repo))
			return result, err
		}
	} else {
		_ = os.Remove(opts.RepoDir)
		result.CheckoutResult = append(result.CheckoutResult, fmt.Sprintf("CLONE %s", opts.Repo))
		args := []string{"clone"}
		failLabel := "git clone"
		if shallow {
			args = append(args, "--depth", "1")
			failLabel = "git clone --depth 1"
		}
		args = append(args, opts.RemoteURL, opts.RepoDir)
		out, err := runCommand("", "git", args...)
		result.CheckoutResult = append(result.CheckoutResult, out...)
		if err != nil {
			result.CheckoutResult = append(result.CheckoutResult, fmt.Sprintf("FAIL %s: %s", opts.Repo, failLabel))
			return result, err
		}
	}

	if opts.Branch != "" && dirExists(gitDir) {
		result.BranchResult = append(result.BranchResult, fmt.Sprintf("CHECKOUT %s %s", opts.Repo, opts.Branch))
		out, err := runCommand(opts.RepoDir, "git", "-C", opts.RepoDir, "checkout", "-B", opts.Branch)
		result.BranchResult = append(result.BranchResult, out...)
		if err != nil {
			result.BranchResult = append(result.BranchResult, fmt.Sprintf("FAIL %s: git checkout -B %s", opts.Repo, opts.Branch))
			return result, err
		}
		result.BranchReady = fmt.Sprintf("%s:%s", opts.Repo, opts.Branch)
	} else {
		result.BranchResult = append(result.BranchResult, fmt.Sprintf("SKIP %s: branch setup unavailable", opts.Repo))
	}

	if fileExists(filepath.Join(opts.RepoDir, "go.mod")) {
		if opts.RunMode == RunModeService {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("defer go module bootstrap for service startup: %s", opts.RepoDir))
			result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: defer go module bootstrap to service startup", opts.Repo))
		} else if opts.RunMode == RunModeSmoke {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip go module bootstrap for smoke verification: %s", opts.RepoDir))
			result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: smoke verification skips go module bootstrap", opts.Repo))
		} else {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("go -C %s mod download", opts.RepoDir))
			if commandExists("go") {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("GO_MOD_DOWNLOAD %s", opts.Repo))
				out, err := runCommand(opts.RepoDir, "go", "mod", "download")
				result.BootstrapResult = append(result.BootstrapResult, out...)
				if err != nil {
					result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("FAIL %s: go mod download", opts.Repo))
				}
			} else {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: go unavailable", opts.Repo))
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
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("PNPM_INSTALL %s", opts.Repo))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "install", "--ignore-scripts")
				result.BootstrapResult = append(result.BootstrapResult, out...)
				if err != nil {
					result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("FAIL %s: pnpm install --ignore-scripts", opts.Repo))
				}
			} else if opts.RunMode == RunModeSmoke {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: smoke verification skips pnpm bootstrap", opts.Repo))
			} else {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("PNPM_FETCH %s", opts.Repo))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "fetch")
				result.BootstrapResult = append(result.BootstrapResult, out...)
				if err != nil {
					result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("FAIL %s: pnpm fetch", opts.Repo))
				}
			}
		} else {
			result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: pnpm unavailable", opts.Repo))
		}
	} else if fileExists(filepath.Join(opts.RepoDir, "package.json")) {
		if opts.RunMode == RunModeSmoke {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("skip pnpm bootstrap for smoke verification: %s", opts.RepoDir))
			result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: smoke verification skips pnpm bootstrap", opts.Repo))
		} else {
			result.BootstrapPlan = append(result.BootstrapPlan, fmt.Sprintf("pnpm --store-dir %s --config.state-dir %s --dir %s install --ignore-scripts", pnpmStoreDir(opts.PNPMStoreDir), pnpmStateDir(opts.PNPMStateDir), opts.RepoDir))
			if commandExists("pnpm") {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("PNPM_INSTALL %s", opts.Repo))
				_ = cleanupPNPMProjectLinks(pnpmStoreDir(opts.PNPMStoreDir))
				out, err := runCommand("", "pnpm", "--store-dir", pnpmStoreDir(opts.PNPMStoreDir), "--config.state-dir="+pnpmStateDir(opts.PNPMStateDir), "--dir", opts.RepoDir, "install", "--ignore-scripts")
				result.BootstrapResult = append(result.BootstrapResult, out...)
				if err != nil {
					result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("FAIL %s: pnpm install --ignore-scripts", opts.Repo))
				}
			} else {
				result.BootstrapResult = append(result.BootstrapResult, fmt.Sprintf("SKIP %s: pnpm unavailable", opts.Repo))
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

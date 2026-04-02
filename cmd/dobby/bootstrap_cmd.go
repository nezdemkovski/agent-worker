package main

import "github.com/nezdemkovski/agent-worker/internal/worker"

func runBootstrapRepo(args []string) int {
	fs := flagSet("bootstrap-repo")

	repo := fs.String("repo", "", "repo name")
	repoDir := fs.String("repo-dir", "", "repo checkout path")
	remoteURL := fs.String("remote-url", "", "remote clone url")
	branch := fs.String("branch", "", "branch to checkout")
	runMode := fs.String("run-mode", "", "run mode")
	pnpmStoreDir := fs.String("pnpm-store-dir", "", "pnpm store dir")
	pnpmStateDir := fs.String("pnpm-state-dir", "", "pnpm state dir")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	result, err := worker.BootstrapRepo(worker.BootstrapRepoOptions{
		Repo:         *repo,
		RepoDir:      *repoDir,
		RemoteURL:    *remoteURL,
		Branch:       *branch,
		RunMode:      worker.RunMode(*runMode),
		PNPMStoreDir: *pnpmStoreDir,
		PNPMStateDir: *pnpmStateDir,
	})
	if err != nil {
		emitJSON(bootstrapRepoResponse{
			Version:         responseVersion,
			Status:          worker.StatusError,
			Reason:          err.Error(),
			ClonePlan:       result.ClonePlan,
			CheckoutOutput:  result.CheckoutOutput,
			CheckoutEvents:  result.CheckoutEvents,
			BranchOutput:    result.BranchOutput,
			BranchEvents:    result.BranchEvents,
			BootstrapPlan:   result.BootstrapPlan,
			BootstrapOutput: result.BootstrapOutput,
			BootstrapEvents: result.BootstrapEvents,
			BranchReady:     result.BranchReady,
			PreparedRepo:    result.PreparedRepo,
		})
		return 1
	}

	emitJSON(bootstrapRepoResponse{
		Version:         responseVersion,
		Status:          worker.StatusOK,
		ClonePlan:       result.ClonePlan,
		CheckoutOutput:  result.CheckoutOutput,
		CheckoutEvents:  result.CheckoutEvents,
		BranchOutput:    result.BranchOutput,
		BranchEvents:    result.BranchEvents,
		BootstrapPlan:   result.BootstrapPlan,
		BootstrapOutput: result.BootstrapOutput,
		BootstrapEvents: result.BootstrapEvents,
		BranchReady:     result.BranchReady,
		PreparedRepo:    result.PreparedRepo,
	})
	return 0
}

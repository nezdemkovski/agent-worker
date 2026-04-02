package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func runPlanStart(args []string) int {
	fs := flagSet("plan-start")
	service := fs.String("service", "", "service name")
	workDir := fs.String("workdir", "", "service working directory")
	profile := fs.String("runtime-profile", "", "runtime profile")
	entrypoint := fs.String("entrypoint", "", "service entrypoint")
	strategy := fs.String("start-strategy", "", "requested start strategy")
	port := fs.String("port", "", "service port")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	plan, err := worker.PlanStart(worker.StartPlanOptions{
		Service:        *service,
		WorkDir:        *workDir,
		RuntimeProfile: worker.RuntimeProfile(*profile),
		EntryPoint:     *entrypoint,
		StartStrategy:  *strategy,
		Port:           *port,
	})
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitJSON(startPlanResponse{
		Version:          responseVersion,
		Status:           worker.StatusOK,
		RuntimeProfile:   plan.RuntimeProfile,
		StartStrategy:    plan.Strategy,
		StartDescription: plan.Description,
		Plan:             plan,
	})
	return 0
}

func runExecPlan(args []string) int {
	fs := flagSet("exec-plan")
	planFile := fs.String("plan-file", "", "path to plan JSON file (default: read from stdin)")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	var (
		planData []byte
		err      error
	)
	if *planFile != "" {
		planData, err = os.ReadFile(*planFile)
	} else {
		planData, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read plan: %v", err)})
		return 1
	}

	plan, err := worker.ParsePlanJSON(planData)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse plan: %v", err)})
		return 1
	}

	if err := worker.ExecPlan(plan); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}
	return 0
}

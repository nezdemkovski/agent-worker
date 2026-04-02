package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

const responseVersion = 1

type errorResponse struct {
	Version    int               `json:"version"`
	Status     string            `json:"status"`
	Reason     string            `json:"reason"`
	ReasonCode worker.ReasonCode `json:"reason_code,omitempty"`
}

type superviseResponse struct {
	Version         int    `json:"version"`
	Status          string `json:"status"`
	PID             int    `json:"pid,omitempty"`
	ReadyURL        string `json:"ready_url,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
}

type terminateResponse struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	PID     int    `json:"pid,omitempty"`
}

type monitorResponse struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	PID     int    `json:"pid,omitempty"`
}

type hashResponse struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
	Hash    string `json:"hash,omitempty"`
}

type restartResponse struct {
	Version         int    `json:"version"`
	Status          string `json:"status"`
	OldPID          int    `json:"old_pid,omitempty"`
	NewPID          int    `json:"new_pid,omitempty"`
	ReadyURL        string `json:"ready_url,omitempty"`
	OldCmdline      string `json:"old_cmdline,omitempty"`
	NewCmdline      string `json:"new_cmdline,omitempty"`
	OldSourceHash   string `json:"old_source_hash,omitempty"`
	NewSourceHash   string `json:"new_source_hash,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
}

type startPlanResponse struct {
	Version          int                    `json:"version"`
	Status           string                 `json:"status"`
	RuntimeProfile   string                 `json:"runtime_profile,omitempty"`
	StartStrategy    string                 `json:"start_strategy,omitempty"`
	StartDescription string                 `json:"start_description,omitempty"`
	Plan             *worker.TypedStartPlan `json:"plan,omitempty"`
}

type bootstrapRepoResponse struct {
	Version         int            `json:"version"`
	Status          string         `json:"status"`
	Reason          string         `json:"reason,omitempty"`
	ClonePlan       []string       `json:"clone_plan,omitempty"`
	CheckoutOutput  []string       `json:"checkout_output,omitempty"`
	CheckoutEvents  []worker.Event `json:"checkout_events,omitempty"`
	BranchOutput    []string       `json:"branch_output,omitempty"`
	BranchEvents    []worker.Event `json:"branch_events,omitempty"`
	BootstrapPlan   []string       `json:"bootstrap_plan,omitempty"`
	BootstrapOutput []string       `json:"bootstrap_output,omitempty"`
	BootstrapEvents []worker.Event `json:"bootstrap_events,omitempty"`
	BranchReady     string         `json:"branch_ready,omitempty"`
	PreparedRepo    string         `json:"prepared_repo,omitempty"`
}

func emitJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fallback, _ := json.Marshal(errorResponse{
			Version: responseVersion,
			Status:  worker.StatusError,
			Reason:  "failed to encode JSON output",
		})
		fmt.Fprintln(os.Stdout, string(fallback))
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

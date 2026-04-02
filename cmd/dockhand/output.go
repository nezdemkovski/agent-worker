package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

type errorResponse struct {
	Status     string            `json:"status"`
	Reason     string            `json:"reason"`
	ReasonCode worker.ReasonCode `json:"reason_code,omitempty"`
}

type superviseResponse struct {
	Status          string `json:"status"`
	PID             int    `json:"pid,omitempty"`
	ReadyURL        string `json:"ready_url,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
}

type terminateResponse struct {
	Status string `json:"status"`
	PID    int    `json:"pid,omitempty"`
}

type monitorResponse struct {
	Status string `json:"status"`
	PID    int    `json:"pid,omitempty"`
}

type hashResponse struct {
	Status string `json:"status"`
	Hash   string `json:"hash,omitempty"`
}

type restartResponse struct {
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
	Status           string                 `json:"status"`
	RuntimeProfile   string                 `json:"runtime_profile,omitempty"`
	StartStrategy    string                 `json:"start_strategy,omitempty"`
	StartCommand     string                 `json:"start_command,omitempty"`
	StartDescription string                 `json:"start_description,omitempty"`
	Plan             *worker.TypedStartPlan `json:"plan,omitempty"`
}

func emitJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fallback, _ := json.Marshal(errorResponse{
			Status: worker.StatusError,
			Reason: "failed to encode JSON output",
		})
		fmt.Fprintln(os.Stdout, string(fallback))
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

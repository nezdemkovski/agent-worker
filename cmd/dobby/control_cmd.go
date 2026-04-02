package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func runControl(args []string) int {
	fs := flagSet("control")
	requestFile := fs.String("request-file", "", "path to control request JSON")
	responseFile := fs.String("response-file", "", "path to write control response JSON")
	pidFile := fs.String("pid-file", "", "path to pid file")
	readyURL := fs.String("ready-url", "", "readiness URL")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "readiness timeout")
	logFile := fs.String("log-file", "", "service log file")
	repoDir := fs.String("repo-dir", "", "repo directory for source hash")
	profile := fs.String("profile", "", "runtime profile for source hash")
	planFile := fs.String("plan-file", "", "service start plan JSON file")
	mirrordTarget := fs.String("mirrord-target", "", "mirrord target")
	serviceURL := fs.String("service-url", "", "service base URL")

	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	reqData, err := os.ReadFile(*requestFile)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read request: %v", err)})
		return 1
	}

	var req worker.ControlRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse request: %v", err)})
		return 1
	}
	command := []string{"mirrord", "exec", "--target", *mirrordTarget, "--", "dobby", "exec-plan", "--plan-file", *planFile}
	resp := worker.ExecuteControl(context.Background(), &req, worker.ControlExecOptions{
		Command:      command,
		PIDFile:      *pidFile,
		ServiceURL:   *serviceURL,
		ReadyURL:     *readyURL,
		ReadyTimeout: *readyTimeout,
		LogFile:      *logFile,
		RepoDir:      *repoDir,
		Profile:      worker.RuntimeProfile(*profile),
	})

	respData, err := json.Marshal(resp)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("encode response: %v", err)})
		return 1
	}
	if err := os.WriteFile(*responseFile, respData, 0o644); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("write response: %v", err)})
		return 1
	}

	emitJSON(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}{Version: responseVersion, Status: worker.StatusOK})
	return 0
}

func runSubmitControl(args []string) int {
	fs := flagSet("submit-control")
	controlDir := fs.String("control-dir", "/artifacts/control", "control request directory")
	timeout := fs.Duration("timeout", 180*time.Second, "maximum time to wait for a control response")
	requestFile := fs.String("request-file", "", "path to request JSON file (default: read stdin)")
	requestJSON := fs.String("request-json", "", "request JSON payload")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	var (
		reqData []byte
		err     error
	)
	if strings.TrimSpace(*requestJSON) != "" {
		reqData = []byte(*requestJSON)
	} else if *requestFile != "" {
		reqData, err = os.ReadFile(*requestFile)
	} else {
		reqData, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("read request: %v", err)})
		return 1
	}

	var req worker.ControlRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("parse request: %v", err)})
		return 1
	}
	if strings.TrimSpace(req.RequestID) == "" {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: "request_id is required"})
		return 1
	}
	if err := os.MkdirAll(*controlDir, 0o755); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("mkdir control dir: %v", err)})
		return 1
	}

	reqPath := filepath.Join(*controlDir, fmt.Sprintf("%s-%s.request", req.Action, req.RequestID))
	respPath := strings.TrimSuffix(reqPath, ".request") + ".response"
	_ = os.Remove(reqPath)
	_ = os.Remove(respPath)
	if err := os.WriteFile(reqPath, reqData, 0o644); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("write request: %v", err)})
		return 1
	}

	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(respPath); err == nil {
			fmt.Println(string(data))
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}

	timeoutResp := worker.NewControlErrorResponse(&req, "control.timeout", "timeout waiting for control response")
	respData, err := json.Marshal(timeoutResp)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: fmt.Sprintf("encode timeout response: %v", err)})
		return 1
	}
	fmt.Println(string(respData))
	return 0
}

package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type serviceControlLoop struct {
	artifacts          workerArtifacts
	serviceName        string
	repoDir            string
	profile            RuntimeProfile
	serviceURL         string
	readyURL           string
	readyTimeout       time.Duration
	target             string
	commandBuilder     func(planFile, target string) []string
	serviceResult      *eventLogFile
	mirrordResult      *eventLogFile
	verificationResult *eventLogFile
}

func (l serviceControlLoop) processPendingRequests(ctx context.Context, currentPID *int) (bool, error) {
	matches, err := filepath.Glob(filepath.Join(l.artifacts.ControlDir, "*.request"))
	if err != nil {
		return false, err
	}
	sort.Strings(matches)
	var restarted bool
	for _, requestFile := range matches {
		didRestart, err := l.processRequest(ctx, requestFile, currentPID)
		if err != nil {
			return false, err
		}
		restarted = restarted || didRestart
	}
	return restarted, nil
}

func (l serviceControlLoop) processRequest(ctx context.Context, requestFile string, currentPID *int) (bool, error) {
	reqData, err := os.ReadFile(requestFile)
	if err != nil {
		return false, err
	}
	var req ControlRequest
	if err := json.Unmarshal(reqData, &req); err != nil {
		return false, err
	}
	message, details := describeControlRequest(&req)
	appendControlTimeline(l.serviceResult, &req, "received", message, details)
	resp := ExecuteControl(ctx, &req, ControlExecOptions{
		Command:      l.commandBuilder(l.artifacts.ServiceStartPlan, l.target),
		PIDFile:      l.artifacts.ServicePID,
		ServiceURL:   l.serviceURL,
		ReadyURL:     l.readyURL,
		ReadyTimeout: l.readyTimeout,
		LogFile:      l.artifacts.ServiceLog,
		RepoDir:      l.repoDir,
		Profile:      l.profile,
	})
	responseFile := strings.TrimSuffix(requestFile, ".request") + ".response"
	respData, err := json.Marshal(resp)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(responseFile, respData, 0o644); err != nil {
		return false, err
	}
	_ = os.Remove(requestFile)
	if resp.Status == StatusOK {
		appendControlTimeline(l.serviceResult, &req, "completed", fmt.Sprintf("%s action completed", req.Action), nil)
	} else if resp.Error != nil {
		appendControlTimeline(l.serviceResult, &req, "failed", resp.Error.Message, map[string]string{"error_code": resp.Error.Code})
	}
	if resp.Status == StatusOK && req.Action == ActionRestart {
		return true, l.handleRestartResponse(resp, currentPID)
	}
	return false, nil
}

func (l serviceControlLoop) handleRestartResponse(resp *ControlResponse, currentPID *int) error {
	var restartResult RestartActionResult
	if err := json.Unmarshal(resp.Result, &restartResult); err != nil {
		return err
	}
	*currentPID = restartResult.NewPID
	if err := writeServiceReady(l.artifacts, l.serviceName, l.target, l.serviceURL, l.readyURL, ProbeResult{
		StatusCode: restartResult.StatusCode,
		Headers:    restartResult.ResponseHeaders,
		Body:       restartResult.ResponseBody,
	}); err != nil {
		return err
	}
	_ = l.serviceResult.Append(withService(NewEvent(CodeServiceReady, LevelInfo, l.readyURL), l.serviceName, map[string]string{"ready_url": l.readyURL}))
	_ = l.mirrordResult.Append(withService(NewEvent(CodeMirrordExec, LevelInfo, "service readiness probe"), l.serviceName, nil))
	_ = l.verificationResult.Append(withService(NewEvent(CodeVerificationServiceReady, LevelInfo, l.readyURL), l.serviceName, map[string]string{"ready_url": l.readyURL}))
	return nil
}

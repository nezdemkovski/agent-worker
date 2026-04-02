package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type ControlExecOptions struct {
	Command      []string
	PIDFile      string
	ServiceURL   string
	ReadyURL     string
	ReadyTimeout time.Duration
	LogFile      string
	RepoDir      string
	Profile      RuntimeProfile
}

func ExecuteControl(ctx context.Context, req *ControlRequest, opts ControlExecOptions) *ControlResponse {
	switch req.Action {
	case ActionRestart:
		result, err := Restart(ctx, RestartOptions{
			PIDFile:      opts.PIDFile,
			Command:      opts.Command,
			ReadyURL:     opts.ReadyURL,
			ReadyTimeout: opts.ReadyTimeout,
			LogFile:      opts.LogFile,
			RepoDir:      opts.RepoDir,
			Profile:      opts.Profile,
		})
		if err != nil {
			errCode := "restart.failed"
			var supErr *SuperviseError
			if errors.As(err, &supErr) {
				errCode = "restart." + string(supErr.Code)
			}
			return NewControlErrorResponse(req, errCode, err.Error())
		}
		resp := NewControlResponse(req, StatusOK)
		_ = resp.SetResult(RestartActionResult{
			OldPID:          result.OldPID,
			NewPID:          result.NewPID,
			URL:             opts.ServiceURL,
			ReadyURL:        result.ReadyURL,
			OldSourceHash:   result.OldSourceHash,
			NewSourceHash:   result.NewSourceHash,
			OldCmdline:      result.OldCmdline,
			NewCmdline:      result.NewCmdline,
			StatusCode:      result.Probe.StatusCode,
			ResponseHeaders: result.Probe.Headers,
			ResponseBody:    result.Probe.Body,
		})
		return resp
	case ActionRequest:
		var payload RequestActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return NewControlErrorResponse(req, "request.invalid_payload", err.Error())
		}
		result, err := RunRequestAction(ctx, payload)
		resp := NewControlResponse(req, StatusOK)
		_ = resp.SetResult(result)
		if err != nil {
			resp.Status = StatusError
			resp.Error = &ControlError{Code: "request.failed", Message: err.Error()}
		}
		return resp
	case ActionExec:
		var payload ExecActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return NewControlErrorResponse(req, "exec.invalid_payload", err.Error())
		}
		result, err := RunExecAction(ctx, payload)
		resp := NewControlResponse(req, StatusOK)
		_ = resp.SetResult(result)
		if err != nil {
			resp.Status = StatusError
			resp.Error = &ControlError{Code: "exec.failed", Message: err.Error()}
		}
		return resp
	case ActionPrompt:
		var payload PromptActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return NewControlErrorResponse(req, "prompt.invalid_payload", err.Error())
		}
		result, err := RunPromptAction(ctx, payload)
		resp := NewControlResponse(req, StatusOK)
		_ = resp.SetResult(result)
		if err != nil {
			resp.Status = StatusError
			resp.Error = &ControlError{Code: "prompt.failed", Message: err.Error()}
		}
		return resp
	default:
		return NewControlErrorResponse(req, "unsupported_action", fmt.Sprintf("unsupported control action %q", req.Action))
	}
}

package worker

import (
	"context"
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
	default:
		return NewControlErrorResponse(req, "unsupported_action", fmt.Sprintf("unsupported control action %q", req.Action))
	}
}

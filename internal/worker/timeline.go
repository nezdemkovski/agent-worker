package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

func appendBootstrapTimeline(log *eventLogFile, repo, repoDir string, result *BootstrapRepoResult) {
	_ = log.Append(eventWithRepo(NewEvent(CodeRepoCheckout, LevelInfo, "preparing repository in "+repoDir), repo))
	appendBootstrapTimelineLines(log, repo, result.CheckoutResult)
	appendBootstrapTimelineLines(log, repo, result.BranchResult)
	appendBootstrapTimelineLines(log, repo, result.BootstrapResult)
}

func appendBootstrapTimelineLines(log *eventLogFile, repo string, lines []string) {
	for _, line := range lines {
		event := humanizeBootstrapTimelineLine(repo, line)
		_ = log.Append(event)
	}
}

func humanizeBootstrapTimelineLine(repo, line string) Event {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "repository step completed"), repo)
	}

	switch {
	case strings.HasPrefix(trimmed, "CLONE "+repo):
		return eventWithRepo(NewEvent(CodeRepoClone, LevelInfo, "cloning repository"), repo)
	case strings.HasPrefix(trimmed, "FETCH "+repo):
		return eventWithRepo(NewEvent(CodeRepoCheckout, LevelInfo, "fetching latest changes"), repo)
	case strings.HasPrefix(trimmed, "CHECKOUT "+repo+" "):
		branch := strings.TrimPrefix(trimmed, "CHECKOUT "+repo+" ")
		event := eventWithRepo(NewEvent(CodeRepoBranch, LevelInfo, "checking out branch "+branch), repo)
		event.Details = map[string]string{"branch": branch}
		return event
	case strings.HasPrefix(trimmed, "GO_MOD_DOWNLOAD "+repo):
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "downloading Go modules"), repo)
	case strings.HasPrefix(trimmed, "PNPM_FETCH "+repo):
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "fetching pnpm dependencies"), repo)
	case strings.HasPrefix(trimmed, "PNPM_INSTALL "+repo):
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, "installing pnpm dependencies"), repo)
	case strings.HasPrefix(trimmed, "SKIP "+repo+": "):
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelWarn, strings.TrimPrefix(trimmed, "SKIP "+repo+": ")), repo)
	case strings.HasPrefix(trimmed, "FAIL "+repo+": "):
		return eventWithRepo(NewEvent(CodeRepoFail, LevelError, strings.TrimPrefix(trimmed, "FAIL "+repo+": ")), repo)
	default:
		return eventWithRepo(NewEvent(CodeRepoBootstrap, LevelInfo, trimmed), repo)
	}
}

func appendControlTimeline(log *eventLogFile, req *ControlRequest, status string, message string, details map[string]string) {
	code := CodeControlReceived
	level := LevelInfo
	switch status {
	case "completed":
		code = CodeControlCompleted
	case "failed":
		code = CodeControlFailed
		level = LevelError
	}
	event := withService(NewEvent(code, level, message), req.Service, details)
	if req.Action != "" {
		if event.Details == nil {
			event.Details = map[string]string{}
		}
		event.Details["action"] = string(req.Action)
	}
	_ = log.Append(event)
}

func describeControlRequest(req *ControlRequest) (string, map[string]string) {
	switch req.Action {
	case ActionRestart:
		return "received restart request", nil
	case ActionRequest:
		var payload RequestActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err == nil {
			return fmt.Sprintf("received %s %s", strings.ToUpper(strings.TrimSpace(payload.Method)), strings.TrimSpace(payload.URL)), nil
		}
		return "received request action", nil
	case ActionExec:
		var payload ExecActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err == nil && len(payload.Command) > 0 {
			return "received command execution request", map[string]string{"command": strings.Join(payload.Command, " ")}
		}
		return "received exec action", nil
	case ActionPrompt:
		var payload PromptActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err == nil {
			details := map[string]string{}
			if payload.Tool != "" {
				details["tool"] = payload.Tool
			}
			if payload.Repo != "" {
				details["repo"] = payload.Repo
			}
			return "received prompt request", details
		}
		return "received prompt action", nil
	default:
		return "received control action", nil
	}
}

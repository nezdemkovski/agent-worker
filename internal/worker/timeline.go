package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

func appendBootstrapTimeline(log *eventLogFile, repo, repoDir string, result *BootstrapRepoResult) {
	_ = log.Append(eventWithRepo(NewEvent(CodeRepoCheckout, LevelInfo, "preparing repository in "+repoDir), repo))
	appendBootstrapTimelineEvents(log, result.CheckoutEvents)
	appendBootstrapTimelineEvents(log, result.BranchEvents)
	appendBootstrapTimelineEvents(log, result.BootstrapEvents)
}

func appendBootstrapTimelineEvents(log *eventLogFile, events []Event) {
	for _, event := range events {
		_ = log.Append(event)
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
			if prompt := summarizePromptForLog(payload.Prompt); prompt != "" {
				details["prompt"] = prompt
			}
			return "received prompt request", details
		}
		return "received prompt action", nil
	default:
		return "received control action", nil
	}
}

func summarizePromptForLog(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	prompt = strings.Join(strings.Fields(prompt), " ")
	const maxLen = 280
	if len(prompt) <= maxLen {
		return prompt
	}
	return strings.TrimSpace(prompt[:maxLen-1]) + "…"
}

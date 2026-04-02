package worker

import (
	"context"
	"os"
	"strings"
	"time"
)

func isDirtyIptablesLog(logFile string) bool {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "Detected dirty iptables")
}

func recoverMirrordTarget(ctx context.Context, namespace, service, target, logFile string, serviceResult *eventLogFile) bool {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	deploymentName, ok := deploymentNameFromTarget(target)
	if !ok {
		return false
	}
	if !commandExists("kubectl") {
		return false
	}

	_ = serviceResult.Append(withService(NewEvent(CodeServiceRecover, LevelInfo, "Dobby kills the bad pods for "+deploymentName+", sir"), service, nil))
	deleteResult, err := RunCommand(ctx, "", nil, "", "kubectl", "-n", namespace, "delete", "pod", "-l", "app="+deploymentName, "--wait=false")
	appendCommandResult(logFile, deleteResult)
	if err != nil {
		_ = serviceResult.Append(withService(NewEvent(CodeServiceRecoverFail, LevelError, "Dobby has failed the recovery, sir. Dobby is most ashamed"), service, nil))
		return false
	}

	_ = serviceResult.Append(withService(NewEvent(CodeServiceRecover, LevelInfo, "Dobby waits for deployment/"+deploymentName+" to come back, sir"), service, nil))
	rolloutCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	rolloutResult, err := RunCommand(rolloutCtx, "", nil, "", "kubectl", "-n", namespace, "rollout", "status", "deployment/"+deploymentName, "--timeout=120s")
	appendCommandResult(logFile, rolloutResult)
	if err != nil {
		_ = serviceResult.Append(withService(NewEvent(CodeServiceRecoverFail, LevelError, "Dobby has failed the recovery, sir. Dobby is most ashamed"), service, nil))
		return false
	}
	return true
}

func deploymentNameFromTarget(target string) (string, bool) {
	switch {
	case strings.HasPrefix(target, "deploy/"):
		return strings.TrimPrefix(target, "deploy/"), true
	case strings.HasPrefix(target, "deployment/"):
		return strings.TrimPrefix(target, "deployment/"), true
	default:
		return "", false
	}
}

func appendCommandResult(path string, result *CommandRunResult) {
	if result == nil {
		return
	}
	parts := splitOutputLines(result.Stdout, result.Stderr)
	if len(parts) == 0 {
		return
	}
	appendLines(path, parts)
}

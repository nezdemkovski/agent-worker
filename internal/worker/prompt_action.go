package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func RunPromptAction(ctx context.Context, payload PromptActionPayload) (*PromptActionResult, error) {
	tool := strings.TrimSpace(strings.ToLower(payload.Tool))
	if tool == "" {
		return nil, fmt.Errorf("missing tool")
	}
	repoDir := strings.TrimSpace(payload.RepoDir)
	if repoDir == "" {
		return nil, fmt.Errorf("missing repo dir")
	}
	if _, err := os.Stat(repoDir); err != nil {
		return nil, fmt.Errorf("repo dir: %w", err)
	}

	result := &PromptActionResult{
		Tool:         tool,
		Repo:         payload.Repo,
		PromptSHA256: promptSHA256(payload.Prompt),
	}

	switch tool {
	case "codex":
		if err := ensureCodexLogin(ctx); err != nil {
			return nil, err
		}
		outputFile := filepath.Join(os.TempDir(), "dobby-codex-output-"+result.PromptSHA256)
		defer os.Remove(outputFile)
		result.Command = []string{"codex", "exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "-C", repoDir, "-o", outputFile}
		runResult, err := RunCommand(ctx, repoDir, nil, payload.Prompt, "codex", "exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "-C", repoDir, "-o", outputFile)
		if data, readErr := os.ReadFile(outputFile); readErr == nil {
			result.Output = string(data)
		} else {
			result.Output = runResult.Stdout
		}
		result.Stderr = runResult.Stderr
		result.ExitCode = runResult.ExitCode
		if err != nil && result.ExitCode == 0 {
			result.ExitCode = 1
		}
	case "claude":
		result.Command = []string{"claude", "-p", "--output-format", "json", "--dangerously-skip-permissions", "--add-dir", repoDir, payload.Prompt}
		runResult, err := RunCommand(ctx, repoDir, nil, "", "claude", "-p", "--output-format", "json", "--dangerously-skip-permissions", "--add-dir", repoDir, payload.Prompt)
		result.Output = runResult.Stdout
		result.Stderr = runResult.Stderr
		result.ExitCode = runResult.ExitCode
		if err != nil && result.ExitCode == 0 {
			result.ExitCode = 1
		}
	default:
		result.Stderr = fmt.Sprintf("unsupported tool: %s", tool)
		result.ExitCode = 98
	}

	result.ChangedFiles = promptChangedFiles(ctx, repoDir)
	if result.ExitCode != 0 {
		return result, fmt.Errorf("%s prompt failed with exit code %d", tool, result.ExitCode)
	}
	return result, nil
}

func ensureCodexLogin(ctx context.Context) error {
	if !commandExists("codex") {
		return fmt.Errorf("codex unavailable")
	}
	if _, err := RunCommand(ctx, "", nil, "", "codex", "login", "status"); err == nil {
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil
	}
	_, err := RunCommand(ctx, "", nil, apiKey, "codex", "login", "--with-api-key")
	return err
}

func promptChangedFiles(ctx context.Context, repoDir string) []string {
	if !commandExists("git") {
		return nil
	}
	runResult, err := RunCommand(ctx, repoDir, nil, "", "git", "status", "--short", "--untracked-files=all")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(runResult.Stdout, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func promptSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

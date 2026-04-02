package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func prepareWorkerEnvironment() error {
	if err := ensureGitNetrc(); err != nil {
		return err
	}
	propagateGitTokenToNPM()
	return nil
}

func ensureGitNetrc() error {
	token := strings.TrimSpace(os.Getenv("NDEV_GIT_TOKEN"))
	if token == "" {
		return nil
	}

	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		home = "/root"
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("mkdir home: %w", err)
	}

	netrcPath := filepath.Join(home, ".netrc")
	username := strings.TrimSpace(os.Getenv("NDEV_GIT_USERNAME"))
	if username == "" {
		username = "git"
	}

	content := strings.Join([]string{
		"machine github.com",
		"  login " + username,
		"  password " + token,
		"",
	}, "\n")
	if err := os.WriteFile(netrcPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write .netrc: %w", err)
	}
	return nil
}

func propagateGitTokenToNPM() {
	if strings.TrimSpace(os.Getenv("GH_NPM_TOKEN")) != "" {
		return
	}
	token := strings.TrimSpace(os.Getenv("NDEV_GIT_TOKEN"))
	if token == "" {
		return
	}
	_ = os.Setenv("GH_NPM_TOKEN", token)
}

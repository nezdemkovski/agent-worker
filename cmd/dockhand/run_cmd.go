package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nezdemkovski/agent-worker/internal/worker"
)

func runRun(args []string) int {
	fs := flagSet("run")
	payloadFile := fs.String("payload-file", "", "path to worker payload JSON")
	workspaceDir := fs.String("workspace-dir", "", "workspace root directory")
	artifactsDir := fs.String("artifacts-dir", "", "artifacts directory")
	if err := fs.Parse(args); err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 2
	}

	payload, err := readRunPayload(*payloadFile)
	if err != nil {
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}

	emitRunPrelude(payload, *payloadFile, *workspaceDir, *artifactsDir)

	err = worker.Run(context.Background(), worker.RunOptions{
		PayloadPath:         *payloadFile,
		WorkspaceDir:        *workspaceDir,
		ArtifactsDir:        *artifactsDir,
		DefaultServicePort:  os.Getenv("NDEV_SERVICE_PORT"),
		DefaultReadyPath:    os.Getenv("NDEV_SERVICE_READY_PATH"),
		DefaultReadyTimeout: 180 * time.Second,
	})
	emitArtifactFiles(*artifactsDir)
	if err != nil {
		fmt.Printf("RUN_FAIL %s %s\n", payload.RunID, err.Error())
		fmt.Printf("Run %s failed: %s\n", payload.RunID, err.Error())
		emitJSON(errorResponse{Version: responseVersion, Status: worker.StatusError, Reason: err.Error()})
		return 1
	}
	fmt.Printf("RUN_DONE %s\n", payload.RunID)
	fmt.Printf("Run %s completed successfully\n", payload.RunID)
	emitJSON(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
	}{Version: responseVersion, Status: worker.StatusOK})
	return 0
}

func readRunPayload(path string) (*worker.WorkerPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	var payload worker.WorkerPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	return &payload, nil
}

func emitRunPrelude(payload *worker.WorkerPayload, payloadFile, workspaceDir, artifactsDir string) {
	fmt.Printf("RUN_START %s\n", payload.RunID)
	fmt.Printf("Starting run %s in %s mode\n", payload.RunID, payload.Mode)
	fmt.Printf("Session: %s\n", payload.SessionID)
	fmt.Printf("Payload file: %s\n", payloadFile)
	fmt.Printf("Workspace dir: %s\n", workspaceDir)
	fmt.Printf("Artifacts dir: %s\n", artifactsDir)
}

func emitArtifactFiles(root string) {
	paths, err := collectArtifactFiles(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emit artifacts: %v\n", err)
		return
	}
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Printf("ARTIFACT_FILE_BEGIN %s\n", rel)
		if len(data) > 0 {
			fmt.Print(string(data))
			if data[len(data)-1] != '\n' {
				fmt.Println()
			}
		}
		fmt.Printf("ARTIFACT_FILE_END %s\n", rel)
	}
}

func collectArtifactFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

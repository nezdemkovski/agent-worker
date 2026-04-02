# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`agent-worker` — a standalone container image for in-cluster coding-agent jobs. The core is `dockhand`, a Go CLI that supervises, terminates, restarts, monitors, and fingerprints processes with JSON-only output.

Published to `ghcr.io/nezdemkovski/agent-worker`.

## Build & Test Commands

```bash
# Run all tests
go test ./...

# Run a single test
go test ./internal/worker -run TestSuperviseSucceedsAndWritesArtifacts

# Build the binary locally
go build -o dockhand ./cmd/dockhand

# Build the Docker image locally
./scripts/build.sh agent-worker:dev
```

## Architecture

```
cmd/dockhand/
  main.go       — CLI entry point: flag parsing + subcommand dispatch
  output.go     — JSON response types (EmitSuccess, EmitError)

internal/worker/
  supervise.go  — Start process, poll HTTP readiness endpoint, write PID file
  terminate.go  — SIGTERM → grace period → SIGKILL (process group on Unix)
  restart.go    — Terminate old + Supervise new
  monitor.go    — Poll process liveness at interval; --once for single check
  hash.go       — SHA256 fingerprint of source files (profiles: node-http, go-http, worker-metrics, default)
  process.go    — Process tree utilities: cmdline reading, zombie detection, process group kill
  readiness.go  — HTTP GET readiness check
  constants.go  — Typed enums: ProcessState, ReasonCode, StatusString
```

**Key design decisions:**
- Zero external dependencies — stdlib only (`go.mod` has no `require` directives)
- All CLI output is JSON (`EmitSuccess`/`EmitError` in `output.go`); no plain text mode
- Unix process groups (`Setpgid: true`) for clean tree termination; Windows fallback to simple kill
- Custom `SuperviseError` type with `ReasonCode` for machine-parseable error classification
- Exit codes: 0 success, 1 runtime error, 2 usage error

**Hash profiles** select which files to fingerprint:
- `node-http`: `src/**/*.{ts,tsx,js,json}` + package files
- `go-http`: `**/*.go` + `go.mod` + `go.sum`
- `worker-metrics`: same as go-http
- default: top 2 directory levels

## Testing Patterns

Tests use a **helper process pattern**: `TestWorkerHelperProcess` re-executes the test binary as a subprocess (gated by `GO_WANT_HELPER_PROCESS=1` env var) in modes: `serve` (HTTP server), `exit-immediately`, `sleep-forever`. This lets tests exercise real process lifecycle without external binaries.

Some tests are Unix-only and skip on Windows (`t.Skip`). Tests use `t.Parallel()` where safe.

## CI/CD

GitHub Actions (`.github/workflows/publish.yml`) builds and pushes a multi-arch Docker image (amd64 + arm64) to GHCR on every push to master and on version tags (`v*.*.*`).

## Container Image Contents

The final image (based on `node:24.14.0-bookworm-slim`) includes: kubectl, mirrord, air (Go hot-reload), pnpm, Claude Code CLI, Codex CLI, and the `dockhand` binary at `/out/dockhand`.

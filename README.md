# agent-worker

Standalone container image for in-cluster coding-agent jobs.

## Local build

```bash
./scripts/build.sh agent-worker:dev
```

## Published image

By default the GitHub Actions workflow publishes to:

```text
ghcr.io/<owner>/<repo>
```

For this repository that means:

```text
ghcr.io/nezdemkovski/agent-worker
```

#!/usr/bin/env bash

set -euo pipefail

IMAGE_TAG="${1:-agent-worker:dev}"

docker build -t "${IMAGE_TAG}" .

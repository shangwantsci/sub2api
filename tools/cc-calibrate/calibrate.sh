#!/usr/bin/env bash
# Repeatable Claude Code wire-profile calibration job (Docker wrapper).
#
# Runs the shared calibration core (calibrate-run.sh) inside a throwaway `node:22`
# container, so the HOST only needs Docker — nothing is installed on the host. The
# core installs a REAL Claude Code CLI, drives it against a local capture shim over a
# canary sweep, extracts a versioned profile JSON, and runs the fingerprint guard.
# Telemetry is fully disabled and the CLI never talks to the real Anthropic API (base
# URL points at the shim, dummy token). No real pool account is spent.
#
# For a NO-Docker environment (e.g. the docker-compose cc-calibrate sidecar, which is
# already a node container) run calibrate-run.sh directly instead of this wrapper.
#
# Usage:
#   tools/cc-calibrate/calibrate.sh [CC_VERSION] [OUT_DIR]
#     CC_VERSION  npm version/tag of @anthropic-ai/claude-code (default: latest)
#     OUT_DIR     where profile-<version>.json is written (default: ./out)
#
# Requires: docker.
set -euo pipefail

CC_VERSION="${1:-latest}"
OUT_DIR="${2:-$(cd "$(dirname "$0")" && pwd)/out}"
HERE="$(cd "$(dirname "$0")" && pwd)"
MODELS="${CC_CALIBRATE_MODELS:-haiku sonnet opus fable}"
mkdir -p "$OUT_DIR"

docker run --rm \
  -v "$HERE":/cal:ro \
  -v "$OUT_DIR":/out \
  -e CC_VERSION="$CC_VERSION" \
  -e CC_CALIBRATE_MODELS="$MODELS" \
  -e CAL_DIR=/cal \
  -e OUT_DIR=/out \
  node:22-bookworm-slim bash -euo pipefail /cal/calibrate-run.sh

echo "calibration done -> $OUT_DIR/profile-*.json"

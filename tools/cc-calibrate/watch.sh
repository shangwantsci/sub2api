#!/usr/bin/env bash
# Self-update loop for the Claude Code calibration profile.
#
# Checks the published @anthropic-ai/claude-code version; when it changes (or on a
# fixed cadence), re-runs calibrate.sh. The extract step runs the fingerprint guard
# and exits non-zero on drift, so a profile is only "published" (kept in OUT_DIR)
# when the guard passes. On guard failure the previous profile-<ver>.json files are
# left untouched and a loud message is printed for a human to re-derive the salt.
#
# When GATEWAY_URL + ADMIN_API_KEY are set, a freshly calibrated (guard-passing)
# profile is POSTed to the gateway admin API, which validates it again and hot-loads
# it within ~60s — closing the zero-redeploy loop. The gateway itself re-validates
# (schema, UA<->version, fingerprint guard) and rejects anything invalid, so a bad
# profile can never reach the wire.
#
# Multi-version retention: profiles are named profile-<version>.json, so several
# versions coexist in OUT_DIR — the gateway can keep serving personas that claim an
# older version while newer ones are added.
#
# Two calibration modes (same core logic in calibrate-run.sh):
#   - default: host has Docker -> calibrate.sh spins up a throwaway node container.
#   - CC_CALIBRATE_INPROC=1: already inside a node env (e.g. the docker-compose
#     cc-calibrate sidecar) -> run calibrate-run.sh directly, NO nested Docker.
#
# Usage:
#   tools/cc-calibrate/watch.sh [OUT_DIR] [POLL_SECONDS]
#   GATEWAY_URL=https://gw.example.com ADMIN_API_KEY=... tools/cc-calibrate/watch.sh ./out 3600
# Run under systemd/cron; or loop mode when POLL_SECONDS > 0.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${1:-$HERE/out}"
POLL_SECONDS="${2:-0}"
GATEWAY_URL="${GATEWAY_URL:-}"
ADMIN_API_KEY="${ADMIN_API_KEY:-}"
CC_CALIBRATE_INPROC="${CC_CALIBRATE_INPROC:-0}"
mkdir -p "$OUT_DIR"

# publish_profile POSTs a profile JSON file to the gateway admin API using the admin
# API key (x-api-key). Prefers curl; falls back to node (publish.js, uses global
# fetch — handy in the slim node sidecar without curl). No-op with a hint when
# GATEWAY_URL/ADMIN_API_KEY are unset.
publish_profile() {
  local file="$1"
  if [ -z "$GATEWAY_URL" ] || [ -z "$ADMIN_API_KEY" ]; then
    echo "$(date -Is) GATEWAY_URL/ADMIN_API_KEY unset -> keeping $file locally; upload manually in Admin → Settings"
    return 0
  fi
  if command -v curl >/dev/null 2>&1; then
    local url="${GATEWAY_URL%/}/api/v1/admin/settings/claude-calibrated-profile"
    local code
    code="$(curl -sS -o /tmp/cc-calibrate-publish.out -w "%{http_code}" \
      -X POST "$url" \
      -H "x-api-key: $ADMIN_API_KEY" \
      -H 'Content-Type: application/json' \
      --data-binary @"$file" || echo 000)"
    if [ "$code" = "200" ]; then
      echo "$(date -Is) published $file -> gateway ($url) will hot-load within ~60s"
    else
      echo "$(date -Is) !!! publish failed (HTTP $code): $(cat /tmp/cc-calibrate-publish.out 2>/dev/null)" >&2
      return 1
    fi
  elif command -v node >/dev/null 2>&1; then
    node "$HERE/publish.js" "$file" "$GATEWAY_URL" "$ADMIN_API_KEY"
  else
    echo "$(date -Is) !!! neither curl nor node available to publish $file" >&2
    return 1
  fi
}

latest_version() {
  # Prefer local npm; fall back to a throwaway node container (Docker mode only).
  if command -v npm >/dev/null 2>&1; then
    npm view @anthropic-ai/claude-code version 2>/dev/null
  elif [ "$CC_CALIBRATE_INPROC" != "1" ] && command -v docker >/dev/null 2>&1; then
    docker run --rm node:22-bookworm-slim npm view @anthropic-ai/claude-code version 2>/dev/null
  fi
}

calibrate_once() {
  local ver="$1"
  if [ "$CC_CALIBRATE_INPROC" = "1" ]; then
    CC_VERSION="$ver" OUT_DIR="$OUT_DIR" CAL_DIR="$HERE" bash "$HERE/calibrate-run.sh"
  else
    bash "$HERE/calibrate.sh" "$ver" "$OUT_DIR"
  fi
}

run_once() {
  local ver
  # CC_VERSION 显式锁定要标定/伪装的版本；留空或 "latest" 则跟随 npm latest 自动追新。
  if [ -n "${CC_VERSION:-}" ] && [ "${CC_VERSION}" != "latest" ]; then
    ver="${CC_VERSION}"
  else
    ver="$(latest_version)"
  fi
  if [ -z "$ver" ]; then
    echo "$(date -Is) could not resolve latest claude-code version; skipping" >&2
    return 0
  fi
  if [ -f "$OUT_DIR/profile-$ver.json" ]; then
    echo "$(date -Is) profile-$ver.json already exists; up to date"
    return 0
  fi
  echo "$(date -Is) new version $ver detected -> calibrating (inproc=$CC_CALIBRATE_INPROC)"
  if calibrate_once "$ver"; then
    echo "$(date -Is) calibrated + guard passed -> $OUT_DIR/profile-$ver.json"
    publish_profile "$OUT_DIR/profile-$ver.json" || true
  else
    echo "$(date -Is) !!! calibration/guard FAILED for $ver — NOT publishing; existing profiles kept. Human must inspect (salt/index drift?)." >&2
    rm -f "$OUT_DIR/profile-$ver.json" 2>/dev/null || true
    return 1
  fi
}

if [ "$POLL_SECONDS" -gt 0 ]; then
  while true; do
    run_once || true
    sleep "$POLL_SECONDS"
  done
else
  run_once
fi

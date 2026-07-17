#!/usr/bin/env bash
# Calibration core — assumes it already runs INSIDE a Node.js environment (node + npm
# on PATH). This is what actually installs the real Claude Code CLI, drives it against
# the local capture shim over a canary sweep, and extracts profile-<version>.json.
#
# It is used two ways with the SAME code:
#   1) calibrate.sh runs it inside a throwaway `node:22` container (host needs Docker).
#   2) The docker-compose `cc-calibrate` sidecar runs it directly (no nested Docker),
#      via watch.sh with CC_CALIBRATE_INPROC=1.
#
# Telemetry is fully disabled and a DUMMY token is used pointed at the local shim, so
# the CLI never contacts the real Anthropic API and NO pool account is spent.
#
# Env:
#   CC_VERSION            npm version/tag to calibrate (default: latest)
#   CAL_DIR               dir containing shim.js + extract-profile.js (default: this dir)
#   OUT_DIR               where profile-<version>.json is written (default: ./out)
#   CC_CALIBRATE_MODELS   model sweep (default: "haiku sonnet opus fable")
#   SHIM_PORT             local shim port (default: 8788)
set -euo pipefail

CC_VERSION="${CC_VERSION:-latest}"
CAL_DIR="${CAL_DIR:-$(cd "$(dirname "$0")" && pwd)}"
OUT_DIR="${OUT_DIR:-$(pwd)/out}"
MODELS="${CC_CALIBRATE_MODELS:-haiku sonnet opus fable}"
SHIM_PORT="${SHIM_PORT:-8788}"
mkdir -p "$OUT_DIR"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"
npm init -y >/dev/null 2>&1
npm i "@anthropic-ai/claude-code@${CC_VERSION}" >/dev/null 2>&1
CLA="$WORK/node_modules/.bin/claude"
VER="$("$CLA" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ -z "$VER" ]; then
  echo "could not resolve installed claude-code version" >&2
  exit 1
fi
echo "installed claude-code $VER"

export HOME="$WORK"
export ANTHROPIC_BASE_URL="http://127.0.0.1:${SHIM_PORT}"
export ANTHROPIC_API_KEY=sk-ant-dummy-calibrate-000
export ANTHROPIC_AUTH_TOKEN=dummy-calibrate-000
# Telemetry / phone-home fully off (usage/error/updater/feedback + growthbook).
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
export DISABLE_TELEMETRY=1 DISABLE_ERROR_REPORTING=1 DISABLE_AUTOUPDATER=1 DISABLE_BUG_COMMAND=1
export IS_SANDBOX=1 CI=1

rm -f "$WORK/cap.jsonl"
CAP_OUT="$WORK/cap.jsonl" SHIM_PORT="$SHIM_PORT" node "$CAL_DIR/shim.js" &
SHIM=$!
trap 'kill $SHIM 2>/dev/null || true; rm -rf "$WORK"' EXIT
sleep 1
for M in $MODELS; do
  # tools-triggering turn (exercises the tools beta set)
  timeout 60 "$CLA" -p "Read the file /etc/hostname and tell me its exact contents" \
    --model "$M" --dangerously-skip-permissions >/dev/null 2>&1 || true
  # plain no-tools turn (exercises the base beta set + title-gen sub-calls)
  timeout 60 "$CLA" -p "In one short sentence, what is 2+2?" \
    --model "$M" --dangerously-skip-permissions >/dev/null 2>&1 || true
done
sleep 1
kill $SHIM 2>/dev/null || true

# extract-profile.js exits non-zero (3) on fingerprint-guard failure; set -e then aborts
# and the caller (watch.sh) discards the profile. The file is written either way.
node "$CAL_DIR/extract-profile.js" "$WORK/cap.jsonl" "$VER" > "$OUT_DIR/profile-$VER.json"
echo "wrote $OUT_DIR/profile-$VER.json"
grep -m1 salt_verified "$OUT_DIR/profile-$VER.json" || true

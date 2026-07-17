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
#   CC_CALIBRATE_KEEP_CAP  1 = keep cap-<version>.jsonl in OUT_DIR for diagnostics
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

# Official Claude Code uses two fingerprint inputs:
# - main query: first non-meta user message BEFORE wire normalization;
# - side query: first user text in the side-query API body.
# The former cannot be reconstructed from wire bytes alone, so write a marker before
# each CLI invocation. extract-profile.js tries both the marker prompt and wire user
# texts, and records which source matched the real billing fingerprint.
mark_canary() {
  CANARY_MODEL="$1" CANARY_KIND="$2" CANARY_PROMPT="$3" CAP_OUT="$WORK/cap.jsonl" \
    node -e 'const fs=require("fs"); fs.appendFileSync(process.env.CAP_OUT, JSON.stringify({kind:"canary",model:process.env.CANARY_MODEL,canary_kind:process.env.CANARY_KIND,prompt:process.env.CANARY_PROMPT})+"\n")'
}

# GNU coreutils calls it timeout; Homebrew calls it gtimeout. In an environment with
# neither (plain macOS), run directly—the local shim responds immediately.
run_cli() {
  if command -v timeout >/dev/null 2>&1; then
    timeout 60 "$@"
  elif command -v gtimeout >/dev/null 2>&1; then
    gtimeout 60 "$@"
  else
    "$@"
  fi
}

for M in $MODELS; do
  # tools-triggering turn (exercises the tools beta set)
  PROMPT="Read the file /etc/hostname and tell me its exact contents"
  mark_canary "$M" tools "$PROMPT"
  run_cli "$CLA" -p "$PROMPT" \
    --model "$M" --dangerously-skip-permissions >/dev/null 2>&1 || true
  # plain no-tools turn (exercises the base beta set + title-gen sub-calls)
  PROMPT="In one short sentence, what is 2+2?"
  mark_canary "$M" plain "$PROMPT"
  run_cli "$CLA" -p "$PROMPT" \
    --model "$M" --dangerously-skip-permissions >/dev/null 2>&1 || true
done
sleep 1
kill $SHIM 2>/dev/null || true

# extract-profile.js exits non-zero (3) on fingerprint-guard failure; set -e then aborts
# and the caller (watch.sh) discards the profile. The file is written either way.
if [ "${CC_CALIBRATE_KEEP_CAP:-0}" = "1" ]; then
  cp "$WORK/cap.jsonl" "$OUT_DIR/cap-$VER.jsonl"
fi
node "$CAL_DIR/extract-profile.js" "$WORK/cap.jsonl" "$VER" > "$OUT_DIR/profile-$VER.json"
echo "wrote $OUT_DIR/profile-$VER.json"
grep -m1 salt_verified "$OUT_DIR/profile-$VER.json" || true

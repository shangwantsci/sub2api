# cc-calibrate — Claude Code wire-profile calibration harness

Automated "truth source" for the Claude Code mimic profile. Instead of hand-editing
`backend/internal/pkg/claude/constants.go` every time the official CLI updates, run a
REAL Claude Code CLI against a local capture shim and let it emit its exact wire bytes.

## What it does

1. Installs `@anthropic-ai/claude-code` (a given version/tag) inside a throwaway
   `node:22` container — nothing touches the host.
2. Runs it against a local HTTP capture shim (`ANTHROPIC_BASE_URL` -> shim, dummy token).
   The CLI sends its full request headers+body before it would get a 401, so **no real
   Anthropic account is spent**.
3. Sweeps a canary matrix (models x request features) so per-model / per-feature
   variation (beta sets, thinking, structured-outputs, tools) is captured.
4. Extracts a versioned `profile-<version>.json` (headers template, `absent` headers
   the CLI does NOT send, beta-rule matrix) and runs a **fingerprint regression guard**:
   recomputes the `cc_version=X.Y.Z.{fp}` fingerprint from each canary's first user text
   using the repo algorithm+salt and compares to what the real CLI sent. Mismatch = the
   salt/index algorithm drifted -> guard fails loudly (exit 3) instead of shipping a
   wrong fingerprint.

## Profile schema

`profile-<version>.json` is the machine-readable truth source the gateway loads at
runtime (`backend/internal/pkg/claude/calibrated_profile.go`). Shape:

```jsonc
{
  "schema_version": 1,                 // must match claude.CalibratedProfileSchemaVersion
  "cli_version": "2.1.211",            // must appear inside headers.template User-Agent
  "captured_at": "2026-...Z",
  "source": "cc-calibrate",
  "headers": {
    "template": { "User-Agent": "...", "X-Stainless-OS": "Linux", ... },
    "absent":   ["x-client-request-id"]   // headers the real CLI never sends
  },
  "beta_rules": {
    // key = "<endpoint>|<family>|<features>"
    //   endpoint = messages | count_tokens   (different beta sets; kept separate)
    //   family   = haiku | fable | sonnet | opus
    //   features = sorted "+"-join of {json_schema, tools}  ("" = base)
    "messages|sonnet|":            ["claude-code-...", "interleaved-thinking-..."],
    "messages|sonnet|tools":       ["...", "context-management-..."],
    "count_tokens|sonnet|":        ["...", "token-counting-..."]
  },
  "guard": { "salt_verified": true, "checked": 8, "ok": 8 }
}
```

The gateway resolves betas by exact `endpoint|family|features` first, then relaxes
features to the same-family baseline, and finally falls back to compiled-in constants.
`count_tokens` never falls back to `messages`. A profile with `salt_verified=false`,
a `cli_version` not matching its own User-Agent, or a wrong `schema_version` is
**rejected** by the loader (constants stay authoritative).

Telemetry is fully disabled (`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` + individual
opt-outs), matching how the gateway/sandbox personas run, so the captured profile equals
what real personas emit.

## Usage

```bash
# needs docker on the host
tools/cc-calibrate/calibrate.sh              # latest published version
tools/cc-calibrate/calibrate.sh 2.1.211      # a specific version
tools/cc-calibrate/calibrate.sh latest ./out # custom output dir
```

Output: `out/profile-<version>.json`. Publish it to the gateway (which stores it in the
`claude_code_calibrated_profile` setting and hot-loads it on the next request, ~60s
cache): either paste/upload the JSON in Admin → Settings → Gateway, or POST it to the
admin API (used by `watch.sh` for zero-touch updates):

```bash
curl -sS -X POST "$GATEWAY_URL/api/v1/admin/settings/claude-calibrated-profile" \
  -H "x-api-key: $ADMIN_API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @out/profile-2.1.211.json
```

(`x-api-key` is the Admin API Key from Admin → Settings; a browser JWT via
`Authorization: Bearer` also works.)

`constants.go` remains the compiled-in last-known-good fallback: if no profile is
published (or the published one fails validation), the gateway keeps emitting the
built-in bytes.

## Files

- `shim.js` — HTTP capture shim (logs each request as one JSON line).
- `extract-profile.js` — cap.jsonl -> profile JSON + fingerprint guard.
- `calibrate-run.sh` — the calibration core; assumes it runs inside a Node env
  (installs the CLI, drives the sweep, extracts). Shared by both modes below.
- `calibrate.sh` — Docker wrapper: runs `calibrate-run.sh` in a throwaway `node:22`
  container (host only needs Docker).
- `publish.js` — POST a profile to the gateway admin API using Node's fetch (no curl).
- `watch.sh` — self-update loop (detect new version → calibrate → publish).

## Fully automatic on the production server (recommended)

`deploy/docker-compose.yml` ships an **opt-in `cc-calibrate` sidecar** that runs on the
same host as the gateway and closes the whole loop with zero manual steps after a
one-time setup: it follows new Claude Code CLI versions, calibrates the profile (real
CLI, dummy token, no account spent), and auto-publishes it to the gateway (hot-loaded
in ~60s). It runs the calibration **in-process inside its own node container**
(`CC_CALIBRATE_INPROC=1`), so the host does **not** need Docker-in-Docker.

Enable once:

```bash
# 1) Admin → Settings → Gateway: create/copy the Admin API Key
# 2) put it in .env
echo "CC_CALIBRATE_ADMIN_API_KEY=<key>" >> deploy/.env
# 3) start the sidecar (default profile stays untouched without --profile)
docker compose --profile calibrate up -d
```

Tunables (`.env`): `CC_CALIBRATE_POLL_SECONDS` (default 21600 = 6h),
`CC_CALIBRATE_MODELS`, `CC_CALIBRATE_GATEWAY_URL` (default `http://sub2api:8080`).

## Self-update loop (manual / other hosts)

`watch.sh` is the same loop as a standalone script — run it via cron/systemd on any
host, or on a laptop/CI. Two modes:

```bash
# Docker mode (host has Docker; spins up a throwaway node container per run)
GATEWAY_URL="https://gw.example.com" ADMIN_API_KEY="..." \
  tools/cc-calibrate/watch.sh ./out 3600

# In-process mode (already inside a node env; no nested Docker)
CC_CALIBRATE_INPROC=1 GATEWAY_URL="..." ADMIN_API_KEY="..." \
  tools/cc-calibrate/watch.sh ./out 3600
```

On guard failure it keeps the current profile untouched and alerts (a human must
re-derive the salt/index). If `GATEWAY_URL`/`ADMIN_API_KEY` are unset, it still
calibrates and keeps the profile file locally for manual upload in Admin → Settings.

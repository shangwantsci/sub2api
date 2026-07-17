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

## Fully automatic via GitHub Actions (recommended for production)

The `claude-calibration` job in `.github/workflows/release.yml` runs every 6 hours on a
GitHub-hosted Linux x64 runner. It installs the latest real CLI, captures it with the
dummy-token shim, requires the fingerprint guard to pass, retains raw capture + profile
as a 14-day artifact, then publishes the profile to the gateway. The production host
does no npm install/CLI execution, so gateway CPU/RAM and TTFT are unaffected.

Configure repository secrets once:

```bash
gh secret set CC_CALIBRATE_GATEWAY_URL \
  --repo shangwantsci/sub2api --body "https://your-gateway.example.com"
gh secret set CC_CALIBRATE_ADMIN_API_KEY \
  --repo shangwantsci/sub2api --body "<Admin API Key>"
```

The scheduled workflow becomes active when this workflow version is present on the
repository default branch. It can also be run manually (without a Git tag):

```bash
gh workflow run release.yml --repo shangwantsci/sub2api --ref custom/prod \
  -f tag=v0.1.156 -f calibrate_only=true -f source_ref=custom/prod
```

## Optional production-host sidecar

`deploy/docker-compose.yml` still includes an opt-in `cc-calibrate` sidecar for hosts
with ample spare CPU/RAM. Do **not** use it on a small shared production host: npm install
and multiple real CLI processes can temporarily increase gateway TTFT. Enable only with
`docker compose --profile calibrate up -d` after configuring `CC_CALIBRATE_ADMIN_API_KEY`.

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

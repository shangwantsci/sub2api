# Anthropic SessionKey Bulk Import Design

## Context

Sub2API already supports creating Anthropic accounts from a single `sessionKey` through `POST /api/v1/admin/accounts/setup-token-cookie-auth`, and the create-account modal can loop over multiple pasted keys on the frontend. That flow is not enough for large operational imports because progress is tied to the modal session, proxy assignment is fixed to one selected proxy, and the final statistics are limited.

The CPA project has a better operator experience: an asynchronous backend import job, configurable concurrency and delay, automatic proxy selection, progress polling, detailed counts, failure reasons, and recent per-key results. This design brings that experience into Sub2API while keeping Sub2API's existing account model and account settings.

## Goals

- Add a bulk `sessionKey` import entry on the existing admin accounts page, using the modal layout option A.
- Import all accounts as Anthropic `setup-token` accounts by default.
- Let the administrator choose target groups before starting the job.
- Automatically assign one active Sub2API proxy per imported account when no fixed proxy is selected.
- Name imported accounts as `email + subscription type` when those values are available.
- Show CPA-style progress and statistics: total, processed, created, updated, failed, duplicate, rejected, failure reasons, plan counts, and recent item results.
- Preserve Sub2API's existing account create settings as batch defaults.

## Non-Goals

- Do not add a new standalone import center page in the first version.
- Do not create persistent import history tables in the first version.
- Do not change the existing single-account OAuth or setup-token flow.
- Do not alter official default configuration files.

## UX Design

Add a button on the admin accounts page near the existing account creation actions:

- Label: `批量 sessionKey 导入`
- Opens a modal.

The modal contains:

- A textarea for one `sessionKey` per line.
- Existing Anthropic setup-token account settings reused as batch defaults:
  - groups
  - concurrency
  - priority
  - rate multiplier
  - load factor
  - expires at
  - auto pause on expired
  - 5h window cost limit
  - max sessions
  - RPM settings
  - UMQ mode
  - TLS fingerprint settings
  - session ID masking
  - cache TTL override
  - custom base URL
  - temporary unschedulable rules
- Import controls:
  - import concurrency
  - minimum delay
  - maximum delay
  - proxy mode: `auto active proxy` by default, with an optional fixed proxy override
- A progress section after job start:
  - progress bar
  - processed / total
  - created / updated / failed / duplicate / rejected
  - plan distribution
  - top failure reasons
  - recent results with email, plan, action, proxy, and error summary

## Backend Design

Add a new handler file focused on Anthropic session imports, following the style of `account_codex_import.go` and reusing existing account/admin services.

New endpoints under `/api/v1/admin/accounts`:

- `POST /import/anthropic-session`
- `GET /import/anthropic-session/:id`
- `POST /import/anthropic-session/:id/cancel`

The start request includes:

- `session_keys []string`
- `group_ids []int64`
- `fixed_proxy_id *int64`
- `proxy_mode string` with first-version values `auto` and `fixed`
- account defaults matching existing create-account fields
- import execution settings: `job_concurrency`, `delay_min_ms`, `delay_max_ms`
- `update_existing bool`, default `true`
- `confirm_mixed_channel_risk bool`

The job state is kept in memory for the first version. It is acceptable for running job visibility to reset after a backend restart because import jobs are short-lived operator actions. Account rows created before a restart remain persisted.

## Import Flow

For each normalized unique `sessionKey`:

1. Choose proxy:
   - fixed mode uses `fixed_proxy_id`
   - auto mode picks an active, non-expired proxy from Sub2API's proxy table
   - if no active proxy exists, proceed without proxy and report that in the item result
2. Call `OAuthService.CookieAuth` with scope `inference`.
3. Build credentials from returned token info and retain the raw `sessionKey` in credentials as `session_key`.
4. Build extra from token info and selected account settings.
5. Generate account name from `email + subscription type`; if unavailable, fall back to `Anthropic SetupToken #N`.
6. Detect existing accounts by stable identity, preferring `account_uuid`, then `email_address`, then `org_uuid`.
7. If existing and `update_existing` is true, update credentials, extra, proxy, groups, and selected defaults.
8. Otherwise create a new account through `AdminService.CreateAccount`.
9. Update job counters and append a redacted per-item result.

## Naming

Preferred name format:

```text
<email> <plan>
```

Examples:

- `alice@example.com Max`
- `bob@example.com Team`
- `carol@example.com Pro`

If the plan is unknown, use only email. If email is unknown, use `Anthropic SetupToken #<index>`.

## Proxy Assignment

Auto proxy mode uses active proxies from Sub2API's proxy table. Selection should start with a simple least-used strategy based on current account count per proxy where available, falling back to round-robin/random when counts are unavailable. This keeps assignment balanced without adding new tables.

The item result should include proxy ID and redacted proxy label, never the raw proxy password.

## Statistics

The job snapshot returns:

- `id`
- `status`
- `started_at`, `updated_at`, `finished_at`
- `total`
- `processed`
- `created`
- `updated`
- `failed`
- `duplicate`
- `rejected`
- `failure_reasons`
- `plan_counts`
- `proxy_counts`
- `items`

`items` should be capped, for example at 500, to avoid large responses.

## Error Handling

- Empty input returns `400`.
- A second running import returns `409` with the active job ID.
- User cancellation marks the job as `canceled`.
- Per-key errors do not fail the whole job.
- Failure reasons should be normalized into stable buckets such as `unauthorized`, `forbidden`, `rate_limited`, `bad_json`, `network`, `proxy`, and `unknown`.
- Sensitive values, including session keys and proxy passwords, must never be returned in API responses or logs.

## Testing

Backend tests:

- Normalize session keys and count duplicates.
- Reject empty input.
- Start a job and poll progress.
- Cancel a running job.
- Auto proxy assignment uses active proxies only.
- Fixed proxy mode uses the selected proxy.
- Successful import creates Anthropic `setup-token` accounts.
- Existing accounts update instead of duplicate when `update_existing` is true.
- Job statistics count created, updated, failed, duplicate, rejected, plan counts, and failure reasons.
- Responses do not expose raw session keys or proxy passwords.

Frontend tests:

- Modal opens from accounts page.
- Parsed session key count updates from textarea.
- Start button calls the new API with selected groups and batch defaults.
- Progress bar and statistics render from a job snapshot.
- Polling stops on `completed`, `failed`, or `canceled`.

## Deployment Notes

The first implementation is code-only and does not require a database migration. Production rollout should first build a custom image from `custom/prod`, deploy it to the existing Sub2API container, and verify:

- existing accounts remain available
- existing groups and proxies load
- single-account setup-token import still works
- a small batch import with 2-3 keys completes
- no raw session keys or proxy passwords appear in logs or responses

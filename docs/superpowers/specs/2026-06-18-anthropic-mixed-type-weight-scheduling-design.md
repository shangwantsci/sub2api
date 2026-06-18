# Anthropic Mixed Type Weight Scheduling Design

## Background

In one Anthropic group, setup-token accounts are the operator-owned pool, while api-key accounts can represent external upstream pools. The current load-aware scheduler filters by account priority first, then load rate, then LRU. This makes lower-priority api-key accounts receive little or no traffic, so they do not effectively share load when setup-token accounts are under pressure.

The goal is to let administrators explicitly control traffic split between setup-token and api-key pools inside the same group.

## Goals

- Add opt-in weighted scheduling for Anthropic groups that contain setup-token and api-key accounts.
- Keep existing scheduling behavior unchanged unless the group enables the new policy.
- Let the group define total setup-token pool weight and total api-key pool weight.
- Let each api-key account define its own weight within the api-key pool.
- Keep setup-token account selection unchanged inside its pool: existing load-aware selection continues to decide which setup-token account is used.
- Support api-key total weight `0`, meaning external api-key pools do not receive traffic.

## Non-Goals

- Do not add per-account weights for setup-token accounts.
- Do not replace the whole Anthropic scheduler with the OpenAI scoring scheduler.
- Do not change model routing, quota checks, RPM checks, session limits, or account health semantics.
- Do not change behavior for OpenAI, Gemini, or Antigravity scheduling.

## Configuration Model

Add group-level Anthropic scheduling fields:

- `anthropic_mixed_type_weight_enabled`: boolean, default `false`.
- `anthropic_setup_token_pool_weight`: integer, default `100`.
- `anthropic_api_key_pool_weight`: integer, default `0`.

Add api-key account-level field:

- `pool_weight`: integer, default `1`.

Validation:

- Group pool weights must be `>= 0`.
- At least one pool with schedulable accounts and positive effective weight must be selectable. If the preferred weighted pool has no candidates, the scheduler may fall back to the other pool.
- Api-key account `pool_weight` must be `>= 0`; `0` excludes that api-key account from weighted api-key selection while leaving it available to legacy scheduling if the group policy is disabled.

## Scheduling Flow

When the request is an Anthropic group request and mixed type weight scheduling is enabled:

1. Build the same candidate set as today using existing checks:
   account status, schedulable flag, platform, model support, quota, window cost, RPM, excluded IDs, and group membership.
2. Split candidates into setup-token and api-key pools.
3. Compute effective pool weights:
   - setup-token pool uses `anthropic_setup_token_pool_weight`.
   - api-key pool uses `anthropic_api_key_pool_weight`.
   - a pool with no eligible candidates is temporarily treated as weight `0`.
4. Pick one pool by weighted random selection.
5. Select an account inside the chosen pool:
   - setup-token pool: use current load-aware priority/load/LRU logic unchanged.
   - api-key pool: first consider only api-key accounts with positive `pool_weight`, then combine account weight with existing load-aware filters.
6. If the chosen pool cannot acquire an account slot, try the other eligible pool before returning a wait plan or no-available-accounts error.
7. Preserve current sticky-session behavior before Layer 2 unless the sticky account fails existing schedulability, slot, queue, or session-limit checks.

Weighted random selection means weights are relative proportions. For example, setup-token `100` and api-key `25` targets about 80 percent setup-token traffic and 20 percent api-key traffic over time.

## Api-Key Pool Selection

Inside the api-key pool, selection should remain load-aware but respect administrator weights:

- Filter out api-key accounts with `pool_weight <= 0`.
- Among available api-key candidates, prefer lower load rates first.
- Within similar load conditions, choose by account weight.
- Preserve LRU/random tie-breaking where weights and load are equal.

This keeps unhealthy or full api-key accounts from receiving traffic only because they have high weight.

## Sticky Sessions

Sticky-session behavior remains conservative:

- Existing sticky account is honored if it passes all current checks and can acquire a slot or acceptable sticky wait plan.
- Weighted pool selection applies when sticky selection does not return an account.
- New sticky bindings are written for whichever account is selected by weighted scheduling.

This avoids breaking ongoing sessions while still improving distribution for new or non-sticky requests.

## Fallback Behavior

Fallback rules:

- If api-key pool weight is `0`, api-key accounts are not selected by the new policy.
- If weighted selection picks api-key but no api-key account is available, try setup-token.
- If weighted selection picks setup-token but no setup-token account is available, try api-key only when api-key pool weight is positive.
- If both pools fail to acquire immediate slots, use existing wait-plan behavior with the best candidate from the attempted pools.

## Admin UI

Group settings should expose:

- Enable mixed setup-token/api-key weighted scheduling.
- Setup-token pool weight.
- Api-key pool weight.

Account edit/create should expose api-key account pool weight only when account type is `apikey`. Setup-token accounts should not show this field.

## Testing Plan

Backend unit tests:

- Default disabled policy preserves current selection behavior.
- Enabled policy with api-key pool weight `0` selects only setup-token accounts.
- Enabled policy with setup-token `100` and api-key `100` selects both pools over repeated selections.
- Api-key account weight `0` excludes that api-key account when the policy is enabled.
- If selected api-key pool has no usable candidate, scheduler falls back to setup-token.
- If setup-token pool is unavailable and api-key pool weight is positive, api-key can serve the request.
- Sticky-session hit still wins before weighted selection.

Frontend tests:

- Group form persists mixed-type scheduling fields.
- Api-key account form shows pool weight.
- Setup-token account form hides pool weight.

## Rollout

- Defaults preserve current production behavior.
- Deploy code with the policy disabled.
- Enable policy only on selected groups.
- Start with api-key pool weight `0`, then raise gradually, for example `10`, `25`, `50`, while observing request share, 429 rate, and upstream latency.

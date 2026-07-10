# Upstream 0.1.150 Main Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the frozen official `upstream/main@6dd3274a` into the fork while preserving every documented fork invariant and producing a reviewed, locally verified `0.1.150` merge branch.

**Architecture:** Perform one atomic three-way merge in an isolated worktree because Git cannot review or commit partially resolved conflict groups. Resolve the six textual conflicts line by line, regenerate Ent from the merged schemas, then run separate runtime, product-contract, migration, and full-regression gates with fresh reviewers between gates.

**Tech Stack:** Git merge/worktrees, Go 1.26.5, Ent ORM, PostgreSQL SQL migrations, Vue 3, TypeScript, Vitest, Vite, pnpm 9.15.9 bootstrapped through local `npm exec`, Docker multi-stage build definitions.

## Global Constraints

- Fork implementation baseline is `498903dea914ea74c8568842fde39ba9158dd188`; the execution base is the clean `custom/prod` tip that contains design commit `55b91b531c881a3fc9f4fcf26e0ca3f752f64347` and this committed plan. Task 0 records that immutable tip as `BASE_SHA` before creating the worktree.
- Upstream target is frozen at `6dd3274aafbc1a7a91304380fb3d7e50406841e0`; do not silently advance to a newer `upstream/main`.
- Final `backend/cmd/server/VERSION` must be exactly `0.1.150`.
- Preserve Claude Code HTTP profile `2.1.206`, Fable 5 prompt selection, messages/count_tokens separation, current TLS data, billing metadata, mixed-pool scheduling, Bedrock guard, bulk account import, content safety, and pool-health behavior.
- Do not use `git checkout --ours`, `git checkout --theirs`, whole-file replacement, rebase, squash, or destructive reset.
- Do not push, deploy, run production migrations, or alter production configuration in this plan.
- Keep `batch_image.enabled`, `batch_image.queue_enabled`, and `batch_image.vertex_enabled` at their code defaults of `false`.
- Run every Go command from `backend/` with `GOTOOLCHAIN=go1.26.5`, `GOCACHE=/private/tmp/sub2api-go-build-cache`, and `GOPATH=/private/tmp/sub2api-go-path`; Task 0 must fetch/select Go 1.26.5 before the merge.
- Every source-changing task must use the smallest focused change, run its exact verification, commit normally, and receive specification plus quality review.
- Verification-only tasks create no empty commits; they write ignored reports under `.superpowers/sdd/` and stop on a concrete invariant failure.

---

## File and Responsibility Map

### Merge conflicts

- `backend/ent/group.go`: generated Group fields, scanners, assignments, string rendering.
- `backend/ent/mutation.go`: generated GroupMutation and UsageLogMutation field dispatch.
- `backend/internal/service/api_key_auth_cache_impl.go`: auth snapshot version and Group serialization.
- `backend/internal/service/gateway_forward.go`: strict Claude detection, mimic normalization, metadata, tool rewrite, TLS selection.
- `backend/internal/service/ratelimit_service.go`: no-reset Anthropic 429 cooldown semantics.
- `frontend/src/composables/__tests__/useModelWhitelist.spec.ts`: fork Claude and upstream OpenAI/Grok whitelist coverage.

### Authoritative sources and companion tests

- `backend/ent/schema/group.go`: mixed-pool plus Grok video pricing schema source.
- `backend/ent/schema/usage_log.go`: upstream video usage schema source.
- `backend/ent/generate.go`: canonical Ent generation command.
- `backend/internal/service/api_key_auth_cache.go`: snapshot JSON contract.
- `backend/internal/service/api_key_auth_cache_version_test.go`: rejection of pre-union v14 snapshots.
- `backend/internal/service/api_key_service_cache_test.go`: snapshot round-trip regression.
- `backend/internal/service/rate_limit_429_cooldown_test.go`: runtime-only Anthropic cooldown contract.
- `backend/internal/service/gateway_oauth_metadata_test.go`: strict mimic body, metadata, TLS, and count_tokens coverage.
- `backend/internal/service/gateway_helper_hotpath_test.go`: strict Claude Code context validation.
- `backend/internal/service/admin_service_group_test.go`: mixed-pool group validation.

### Auto-merge semantic review surfaces

- Gateway/handler order: `backend/internal/handler/gateway_handler*.go`, `backend/internal/handler/openai_*handler*.go`, `backend/internal/service/gateway_*.go`.
- Group/cache/scheduler: `backend/internal/repository/{account_repo.go,api_key_repo.go,group_repo.go}`, `backend/internal/service/{account.go,admin_group.go,admin_service.go,gateway_scheduling.go,group.go}`, Group DTO/type files, and generated Ent companions.
- Models/pricing: `backend/resources/model-pricing/model_prices_and_context_window.json`, `backend/internal/service/pricing_service*.go`, `frontend/src/composables/useModelWhitelist.ts`.
- Product routes/i18n: `backend/internal/server/routes/admin.go`, `frontend/src/api/admin/accounts.ts`, `frontend/src/i18n/locales/{en,zh}/admin/accounts.ts`, `frontend/src/router/index.ts`.
- Build/version: `Dockerfile`, `deploy/Dockerfile`, `backend/cmd/server/VERSION`.
- Migrations/config: `backend/migrations/159_*.sql` through `173_*.sql`, `backend/internal/config/config.go`.

---

### Task 0: Create the Isolated Merge Workspace and Record Baseline

**Files:**
- Read: `docs/superpowers/specs/2026-07-10-upstream-0-1-150-main-merge-design.md`
- Read: `docs/superpowers/plans/2026-07-10-upstream-0-1-150-main-merge.md`
- Write report: `.superpowers/sdd/upstream-merge-task-0-baseline.md`

**Interfaces:**
- Consumes: clean `custom/prod` containing `55b91b53` plus this plan commit, and fetched target object `6dd3274a`.
- Produces: isolated branch `merge/upstream-0.1.150-main` and worktree `.worktrees/upstream-0.1.150-main-merge` with a recorded green baseline.

- [ ] **Step 1: Detect the current checkout and verify the worktree directory is ignored**

Run from the main checkout:

```bash
set -euo pipefail
git status --short --branch
test -z "$(git status --porcelain)"
BASE_SHA="$(git rev-parse HEAD)"
test "$(git rev-parse custom/prod)" = "$BASE_SHA"
test "$(git merge-base "$BASE_SHA" 55b91b531c881a3fc9f4fcf26e0ca3f752f64347)" = "55b91b531c881a3fc9f4fcf26e0ca3f752f64347"
test "$(git diff --name-only 55b91b531c881a3fc9f4fcf26e0ca3f752f64347.."$BASE_SHA")" = "docs/superpowers/plans/2026-07-10-upstream-0-1-150-main-merge.md"
git rev-parse upstream/main
git check-ignore -q .worktrees
```

Expected:

- clean `custom/prod`;
- `BASE_SHA` equals the checked-out `custom/prod` tip and descends from design commit `55b91b53`;
- the only path after `55b91b53` is `docs/superpowers/plans/2026-07-10-upstream-0-1-150-main-merge.md`;
- cached upstream target `6dd3274aafbc1a7a91304380fb3d7e50406841e0`;
- ignore check exit `0`.

- [ ] **Step 2: Create the worktree through the worktree skill**

Use `superpowers:using-git-worktrees`. When native worktree tooling is unavailable, run:

```bash
set -euo pipefail
BASE_SHA="$(git rev-parse custom/prod)"
git worktree add .worktrees/upstream-0.1.150-main-merge \
  -b merge/upstream-0.1.150-main \
  "$BASE_SHA"
```

Expected: named branch in the new linked worktree; main checkout remains untouched.

- [ ] **Step 3: Install workspace dependencies without changing lockfiles**

Run in the worktree:

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go version
go mod download
cd ..
npm_config_cache=/private/tmp/sub2api-npm-cache \
  npm exec --yes --package=pnpm@9.15.9 -- \
  pnpm --dir frontend install \
  --frozen-lockfile \
  --prefer-offline \
  --store-dir /private/tmp/sub2api-pnpm-store
test -z "$(git status --porcelain)"
```

Expected: `go version` reports `go1.26.5`; pnpm 9.15.9 honors the repository lock and overrides; dependency setup exits `0`; `git status --short` remains empty. If the toolchain or pnpm package is absent from cache and the sandbox blocks its download, rerun the identical command with approved network access rather than changing package managers or lockfiles.

- [ ] **Step 4: Run the fork baseline tests**

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go test -tags=unit ./internal/pkg/claude -count=1

go test -tags=unit ./internal/service \
  -run 'Test(DefaultClaudeCodeMimicryProfile|ComputeFinalAnthropicBeta|ComputeFinalCountTokensAnthropicBeta|CapturedClaudeCodeExpansionPromptHashes|GatewayService_ClaudeOAuthSyntheticMimic|Handle429_AnthropicNoResetUsesRuntimeCooldownOnly|GatewayService_SelectAccountWithLoadAwareness_Runtime429Block|SelectAccountWithLoadAwareness_MixedTypeWeight|GatewayServiceIsModelSupportedByAccount_Bedrock)' \
  -count=1
cd ..

npm --prefix frontend run test:run -- \
  src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts \
  src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts \
  src/composables/__tests__/useModelWhitelist.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts
```

Expected: all selected tests pass. A sandbox-only loopback bind failure must be recorded verbatim and rerun outside the sandbox before implementation continues.

- [ ] **Step 5: Write the baseline report**

Record exact commands, exits, test counts, warnings, HEADs, and worktree status in `.superpowers/sdd/upstream-merge-task-0-baseline.md`. Do not create a commit for this ignored report.

---

### Task 1: Perform the Atomic Merge and Resolve All Six Conflicts

**Files:**
- Modify through merge: all files changed by `upstream/main@6dd3274a`.
- Resolve: `backend/ent/group.go`
- Resolve: `backend/ent/mutation.go`
- Resolve: `backend/internal/service/api_key_auth_cache_impl.go`
- Resolve: `backend/internal/service/gateway_forward.go`
- Resolve: `backend/internal/service/ratelimit_service.go`
- Resolve: `frontend/src/composables/__tests__/useModelWhitelist.spec.ts`
- Modify test: `backend/internal/service/api_key_auth_cache_version_test.go`
- Modify test: `backend/internal/service/api_key_service_cache_test.go`
- Modify auto-merged test: `backend/internal/service/rate_limit_429_cooldown_test.go`
- Regenerate: `backend/ent/**`
- Write report: `.superpowers/sdd/upstream-merge-task-1-report.md`

**Interfaces:**
- Consumes: Task 0 worktree and frozen upstream commit.
- Produces: a merge commit whose first parent is Task 0's recorded `BASE_SHA` and second parent is `6dd3274a`, with VERSION `0.1.150` and no unresolved conflict markers.

- [ ] **Step 1: Start the frozen merge and verify the conflict inventory**

```bash
set -uo pipefail
set +e
git merge --no-ff --no-commit 6dd3274aafbc1a7a91304380fb3d7e50406841e0
merge_status=$?
set -e
test "$merge_status" = "1"
diff -u \
  <(printf '%s\n' \
    backend/ent/group.go \
    backend/ent/mutation.go \
    backend/internal/service/api_key_auth_cache_impl.go \
    backend/internal/service/gateway_forward.go \
    backend/internal/service/ratelimit_service.go \
    frontend/src/composables/__tests__/useModelWhitelist.spec.ts) \
  <(git diff --name-only --diff-filter=U)
```

Expected conflict list, exactly:

```text
backend/ent/group.go
backend/ent/mutation.go
backend/internal/service/api_key_auth_cache_impl.go
backend/internal/service/gateway_forward.go
backend/internal/service/ratelimit_service.go
frontend/src/composables/__tests__/useModelWhitelist.spec.ts
```

Stop if the list differs; refresh the design instead of resolving an unreviewed target.

- [ ] **Step 2: Confirm the merged Ent schema sources contain both field families**

`backend/ent/schema/group.go` must contain this combined schema contract:

```go
field.Bool("video_rate_independent").Default(false),
field.Float("video_rate_multiplier").
    SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
    Default(1.0),
field.Float("video_price_480p").Optional().Nillable().
    SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
field.Float("video_price_720p").Optional().Nillable().
    SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
field.Float("video_price_1080p").Optional().Nillable().
    SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
field.Bool("anthropic_mixed_type_weight_enabled").Default(false),
field.Int("anthropic_setup_token_pool_weight").Default(100),
field.Int("anthropic_api_key_pool_weight").Default(0),
```

Also confirm `backend/ent/schema/usage_log.go` contains the upstream video count, resolution, and duration fields.

- [ ] **Step 3: Regenerate Ent instead of hand-merging generated dispatch tables**

Run from `backend`:

```bash
GOTOOLCHAIN=go1.26.5 \
GOCACHE=/private/tmp/sub2api-go-build-cache \
GOPATH=/private/tmp/sub2api-go-path \
  go generate ./ent
```

Expected:

- conflict markers in `ent/group.go` and `ent/mutation.go` are replaced by generated code;
- `GroupMutation.Fields()` represents 50 fields;
- generated Group scanners support both bool field families, all video floats, and both mixed-pool integers.

- [ ] **Step 4: Resolve the auth snapshot as a union and bump its schema version**

In `backend/internal/service/api_key_auth_cache.go`, keep both sets:

```go
VideoRateIndependent            bool     `json:"video_rate_independent"`
VideoRateMultiplier             float64  `json:"video_rate_multiplier"`
VideoPrice480P                  *float64 `json:"video_price_480p,omitempty"`
VideoPrice720P                  *float64 `json:"video_price_720p,omitempty"`
VideoPrice1080P                 *float64 `json:"video_price_1080p,omitempty"`
AnthropicMixedTypeWeightEnabled bool     `json:"anthropic_mixed_type_weight_enabled"`
AnthropicSetupTokenPoolWeight   int      `json:"anthropic_setup_token_pool_weight"`
AnthropicAPIKeyPoolWeight       int      `json:"anthropic_api_key_pool_weight"`
```

In `backend/internal/service/api_key_auth_cache_impl.go`, use:

```go
const apiKeyAuthSnapshotVersion = 15 // v15: include group video pricing and Anthropic mixed type weight fields
```

Both `snapshotFromAPIKey` and `snapshotToAPIKey` must assign all eight fields by identical names.

- [ ] **Step 5: Add snapshot union and version-invalidation regressions**

Replace `TestAPIKeyService_SnapshotRoundTrip_PreservesAnthropicMixedTypeWeights` in `backend/internal/service/api_key_service_cache_test.go` with:

```go
func TestAPIKeyService_SnapshotRoundTrip_PreservesAnthropicMixedTypeWeights(t *testing.T) {
    svc := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})
    groupID := int64(9)
    videoPrice480P := 0.11
    videoPrice720P := 0.22
    videoPrice1080P := 0.33
    apiKey := &APIKey{
        ID:      1,
        UserID:  2,
        GroupID: &groupID,
        Key:     "k-anthropic-mixed",
        Name:    "Anthropic Mixed",
        Status:  StatusActive,
        User: &User{
            ID:          2,
            Status:      StatusActive,
            Role:        RoleUser,
            Balance:     10,
            Concurrency: 3,
        },
        Group: &Group{
            ID:                              groupID,
            Name:                            "anthropic",
            Platform:                        PlatformAnthropic,
            Status:                          StatusActive,
            SubscriptionType:                SubscriptionTypeStandard,
            RateMultiplier:                  1,
            VideoRateIndependent:            true,
            VideoRateMultiplier:             1.25,
            VideoPrice480P:                  &videoPrice480P,
            VideoPrice720P:                  &videoPrice720P,
            VideoPrice1080P:                 &videoPrice1080P,
            AnthropicMixedTypeWeightEnabled: true,
            AnthropicSetupTokenPoolWeight:   70,
            AnthropicAPIKeyPoolWeight:       30,
        },
    }

    snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
    roundTrip := svc.snapshotToAPIKey(apiKey.Key, snapshot)

    require.NotNil(t, roundTrip)
    require.NotNil(t, roundTrip.Group)
    require.True(t, roundTrip.Group.VideoRateIndependent)
    require.Equal(t, 1.25, roundTrip.Group.VideoRateMultiplier)
    require.Equal(t, &videoPrice480P, roundTrip.Group.VideoPrice480P)
    require.Equal(t, &videoPrice720P, roundTrip.Group.VideoPrice720P)
    require.Equal(t, &videoPrice1080P, roundTrip.Group.VideoPrice1080P)
    require.True(t, roundTrip.Group.AnthropicMixedTypeWeightEnabled)
    require.Equal(t, 70, roundTrip.Group.AnthropicSetupTokenPoolWeight)
    require.Equal(t, 30, roundTrip.Group.AnthropicAPIKeyPoolWeight)
}
```

Append this test to `backend/internal/service/api_key_auth_cache_version_test.go`:

```go
func TestAPIKeyService_RejectsV14AuthSnapshotWithoutMergedGroupFields(t *testing.T) {
    svc := &APIKeyService{}

    apiKey, ok, err := svc.applyAuthCacheEntry("k-v14", &APIKeyAuthCacheEntry{
        Snapshot: &APIKeyAuthSnapshot{Version: 14},
    })

    if err != nil {
        t.Fatalf("expected stale v14 snapshot to be ignored without error, got %v", err)
    }
    if ok {
        t.Fatal("expected v14 auth snapshot to be rejected after merged group fields were added")
    }
    if apiKey != nil {
        t.Fatalf("expected no API key from stale v14 snapshot, got %#v", apiKey)
    }
}
```

These are post-merge characterization tests: the merge cannot compile until all conflict markers are removed, so a meaningful RED run before conflict resolution is impossible. The required proof is the round-trip union plus explicit v14 rejection under snapshot version 15.

- [ ] **Step 6: Resolve strict Claude mimic behavior with upstream nil guards**

Replace the conflicted mimic section in `backend/internal/service/gateway_forward.go` with this complete merged block:

```go
isClaudeCode := IsClaudeCodeClient(ctx)
shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCode

if shouldMimicClaudeCode {
    systemRewritten := false
    systemRaw, _ := parsed.SystemValue()
    systemPromptInjectionEnabled, systemPrompt, systemPromptBlocks := s.claudeOAuthSystemPromptInjectionSettings(ctx)
    if systemPromptInjectionEnabled {
        if err := replaceBody(rewriteSystemForNonClaudeCodeWithPromptBlocks(body, systemRaw, systemPrompt, systemPromptBlocks)); err != nil {
            return nil, err
        }
        systemRewritten = true
    }

    normalizeOpts := claudeOAuthNormalizeOptions{
        stripSystemCacheControl: !systemRewritten,
        ensureMimicBodyDefaults: account.IsAnthropicOAuthOrSetupToken(),
    }
    if s.identityService != nil && c != nil {
        fp, err := s.identityService.GetOrCreateFingerprint(ctx, account.ID, c.Request.Header)
        if err == nil && fp != nil {
            metadataFP := claudeCodeMimicryFingerprint(fp)
            _, mimicMPT, _ := s.settingService.GetGatewayForwardingSettings(ctx)
            if !mimicMPT {
                if metadataUserID := s.buildOAuthMetadataUserID(parsed, account, metadataFP); metadataUserID != "" {
                    normalizeOpts.injectMetadata = true
                    normalizeOpts.metadataUserID = metadataUserID
                }
            }
        }
    }

    var normalizedBody []byte
    normalizedBody, reqModel = normalizeClaudeOAuthRequestBody(body, reqModel, normalizeOpts)
    if err := replaceBody(normalizedBody); err != nil {
        return nil, err
    }

    if err := replaceBody(s.rewriteMessageCacheControlIfEnabled(ctx, body)); err != nil {
        return nil, err
    }
    if rw := buildToolNameRewriteFromBody(body); rw != nil {
        if err := replaceBody(applyToolNameRewriteToBody(body, rw)); err != nil {
            return nil, err
        }
        if c != nil {
            c.Set(toolNameRewriteKey, rw)
        }
    } else {
        if err := replaceBody(applyToolsLastCacheBreakpoint(body)); err != nil {
            return nil, err
        }
    }
}
```

Do not copy upstream's UA/metadata fallback or Haiku-only rewrite gate. Retain the existing comments, Fable/system prompt selection before this block, count_tokens separation, and `ResolveTLSProfileForClaudeMimic` calls around this block.

- [ ] **Step 7: Resolve Anthropic no-reset 429 as runtime-only cooldown**

The Anthropic branch in `backend/internal/service/ratelimit_service.go` must call exactly:

```go
s.applyAnthropic429RuntimeCooldown(ctx, account, "no_reset_time", responseBody)
```

Do not also call `apply429FallbackRateLimit` for this path.

Keep the final test contract in `backend/internal/service/rate_limit_429_cooldown_test.go`:

```go
require.Zero(t, accountRepo.rateLimitCalls, "无 reset 的 Anthropic 429 不应写入数据库长期限流")
require.True(t, svc.IsAccountRuntimeSchedulingBlocked(context.Background(), account), "无 reset 的 Anthropic 429 应进入短运行时冷却")
```

Retain upstream fallback tests for non-Anthropic platforms; remove only the contradictory Anthropic DB-write expectation.

- [ ] **Step 8: Resolve whitelist tests as a union**

In `frontend/src/composables/__tests__/useModelWhitelist.spec.ts`, preserve:

- fork Sonnet 5 preset pass-through test;
- upstream `gpt-5.6` OpenAI assertion;
- Grok 4.5 model/alias test;
- combined alias mapping test;
- Composer defaults and compatibility aliases;
- `getPresetMappingsByPlatform` import.

The implementation file must retain Claude Sonnet/Fable entries alongside all upstream GPT-5.6/Grok additions.

- [ ] **Step 9: Remove all conflict markers and format changed source**

```bash
set -euo pipefail
if rg -n '^(<<<<<<< .+|>>>>>>> .+)$' .; then
  printf '%s\n' 'unresolved conflict markers remain' >&2
  exit 1
else
  rg_status=$?
  test "$rg_status" = "1"
fi
gofmt -w \
  backend/internal/service/api_key_auth_cache.go \
  backend/internal/service/api_key_auth_cache_impl.go \
  backend/internal/service/api_key_auth_cache_version_test.go \
  backend/internal/service/api_key_service_cache_test.go \
  backend/internal/service/gateway_forward.go \
  backend/internal/service/ratelimit_service.go \
  backend/internal/service/rate_limit_429_cooldown_test.go
git add \
  backend/ent/group.go \
  backend/ent/mutation.go \
  backend/internal/service/api_key_auth_cache_impl.go \
  backend/internal/service/gateway_forward.go \
  backend/internal/service/ratelimit_service.go \
  frontend/src/composables/__tests__/useModelWhitelist.spec.ts
test -z "$(git diff --name-only --diff-filter=U)"
git diff --cached --check
git diff --check
```

Expected: the marker search returns exactly “no matches”; staging the six conflict files clears every unmerged index entry; both staged and unstaged diff checks exit `0`. Do not search for a bare `=======` line because the repository contains legitimate decorative separators. Step 11 still stages all remaining auto-merged and test changes before the merge commit.

- [ ] **Step 10: Run focused conflict-contract tests**

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go test -tags=unit ./internal/service \
  -run 'Test(APIKeyService_(SnapshotRoundTrip_PreservesAnthropicMixedTypeWeights|RejectsV14AuthSnapshotWithoutMergedGroupFields)|Handle429_AnthropicNoResetUsesRuntimeCooldownOnly|GatewayService_ClaudeOAuthSyntheticMimic|GatewayService_AnthropicOAuthCountTokensClaudeMimicBodyDefaultsRemainPre2206|GatewayService_ClaudeMimicTLSProfile|AdminService_.*AnthropicMixedTypeWeights)' \
  -count=1
cd ..

npm --prefix frontend run test:run -- \
  src/composables/__tests__/useModelWhitelist.spec.ts
```

Expected: all selected tests pass.

- [ ] **Step 11: Stage every merge result and create the merge commit**

```bash
set -euo pipefail
git add -A
git diff --cached --check
git status --short
git commit -m "chore: merge upstream 0.1.150"
```

Then verify parent order:

```bash
set -euo pipefail
BASE_SHA="$(git rev-parse custom/prod)"
test "$(git rev-parse HEAD^1)" = "$BASE_SHA"
test "$(git rev-parse HEAD^2)" = "6dd3274aafbc1a7a91304380fb3d7e50406841e0"
```

Expected: both tests exit `0`; the first parent is Task 0's recorded `BASE_SHA`, and the second parent is the frozen upstream target.

- [ ] **Step 12: Write the implementation report and request two-stage review**

Record conflict decisions, generated files, test outputs, merge SHA, parent SHAs, warnings, and worktree status in `.superpowers/sdd/upstream-merge-task-1-report.md`.

Generate the review package from the merge commit's first parent to the merge commit:

```bash
set -euo pipefail
BASE_SHA="$(git rev-parse HEAD^1)"
/Users/asenyu/.codex/plugins/cache/openai-curated-remote/superpowers/6.1.1/skills/subagent-driven-development/scripts/review-package \
  "$BASE_SHA" \
  HEAD
```

Record the unique path printed by the helper. A fresh reviewer must return both specification compliance and code quality verdicts. Critical/Important findings block Task 2.

---

### Task 2: Audit Runtime Ordering and Fork Gateway Invariants

**Files:**
- Review: the P0 gateway/handler/scheduler files listed in the File and Responsibility Map.
- Test: existing gateway, scheduler, 429, Bedrock, Claude profile, TLS, and content-safety suites.
- Write report: `.superpowers/sdd/upstream-merge-task-2-runtime-audit.md`

**Interfaces:**
- Consumes: approved Task 1 merge commit.
- Produces: evidence that upstream protocol/failover changes and fork mimic/safety/scheduling behavior coexist without call-order regressions.

- [ ] **Step 1: Compare all 45 overlap files against both parents**

Reconstruct the overlap inventory, assert its frozen size, and then compare the complete merge result with each parent:

```bash
set -euo pipefail
MERGE_SHA="$(git rev-list --merges --first-parent --max-count=1 HEAD)"
BASE_SHA="$(git rev-parse "${MERGE_SHA}^1")"
UPSTREAM_SHA="$(git rev-parse "${MERGE_SHA}^2")"
COMMON_BASE="6f43986c376d76144cb39c7a562c179e19ac7439"
test "$UPSTREAM_SHA" = "6dd3274aafbc1a7a91304380fb3d7e50406841e0"

OVERLAP_COUNT="$(comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort) | \
  wc -l | tr -d ' ')"
test "$OVERLAP_COUNT" = "45"

comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort)

comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort) | \
  xargs git diff --stat "$BASE_SHA".."$MERGE_SHA" --

comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort) | \
  xargs git diff "$BASE_SHA".."$MERGE_SHA" --

comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort) | \
  xargs git diff --stat "$UPSTREAM_SHA".."$MERGE_SHA" --

comm -12 \
  <(git diff --name-only "$COMMON_BASE".."$BASE_SHA" | sort) \
  <(git diff --name-only "$COMMON_BASE".."$UPSTREAM_SHA" | sort) | \
  xargs git diff "$UPSTREAM_SHA".."$MERGE_SHA" --
```

Expected: the inventory prints exactly 45 paths. In the Task 2 report, mark every gateway/handler/scheduler/rate-limit path from that inventory as reviewed against both parents; Task 3 owns the remaining Ent, Group/cache, model/pricing, route/i18n, type/UI, and Docker paths. Review exact call order for strict client validation, lenient JSON, content safety, slot acquisition, body rewrite, stream commit, in-band error handling, failover, and cooldown. Do not edit during this inspection.

- [ ] **Step 2: Verify fork anchors remain present**

```bash
set -euo pipefail
rg -n '2\.1\.206|cc-2\.1\.206-sdk-cli-macos-arm64|claude-fable-5|claude-sonnet-5' \
  backend/internal/pkg/claude backend/internal/service frontend/src

rg -n 'computeFinalCountTokensAnthropicBeta|BetaTokenCounting|ResolveTLSProfileForClaudeMimic|cc_entrypoint=sdk-cli' \
  backend/internal

rg -n 'AnthropicMixedTypeWeight|PoolWeight|tryAcquireMixedType|IsBedrock' \
  backend/internal backend/ent frontend/src
```

Expected: every design invariant has production-code and test hits.

- [ ] **Step 3: Run focused runtime suites**

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go test -tags=unit \
  ./internal/pkg/claude \
  ./internal/pkg/tlsfingerprint \
  -count=1

go test -tags=unit ./internal/server/routes \
  -run 'TestGatewayRoutes.*(Messages|CountTokens)' \
  -count=1

go test -tags=unit ./internal/repository \
  -run 'MessagesDispatch|TemporaryScheduling|Credential' \
  -count=1

go test -tags=unit ./internal/service \
  -run 'Claude|Mimic|CountTokens|ContentSafety|Runtime429|MixedTypeWeight|Bedrock|Handle429|Failover|ResponseFailed' \
  -count=1
cd ..
```

Expected: all selected packages pass. A loopback-only sandbox failure is rerun outside the sandbox with the identical command.

- [ ] **Step 4: Stop on a semantic violation**

When source review or a test contradicts the design, write the exact file, symbol, failing assertion, and both-parent intent to the report and stop. The controller creates one bounded repair task with a failing test before implementation. Do not make opportunistic edits inside this verification task.

- [ ] **Step 5: Complete the runtime audit report**

Write PASS or FAIL plus exact evidence to `.superpowers/sdd/upstream-merge-task-2-runtime-audit.md`. Create no commit when the gate passes unchanged.

---

### Task 3: Audit Group Cache, Models, Pricing, Product Routes, and Docker Contract

**Files:**
- Review/test: Group schema/generated/cache/DTO/frontend files.
- Review/test: model pricing JSON, whitelist implementation/test, pricing tests.
- Review/test: batch account import API/types/i18n/routes.
- Review: `Dockerfile`, `deploy/Dockerfile`, `backend/cmd/server/VERSION`.
- Write report: `.superpowers/sdd/upstream-merge-task-3-product-contract.md`

**Interfaces:**
- Consumes: approved Task 1 merge commit and Task 2 PASS report.
- Produces: evidence that mixed-pool and upstream video/GPT/Grok contracts coexist across storage, cache, API, UI, pricing, and build layers.

- [ ] **Step 1: Verify Ent regeneration is deterministic**

```bash
set -euo pipefail
cd backend
GOTOOLCHAIN=go1.26.5 \
GOCACHE=/private/tmp/sub2api-go-build-cache \
GOPATH=/private/tmp/sub2api-go-path \
  go generate ./ent
cd ..
git diff --check
git diff --exit-code
```

Expected: no new diff from regeneration. Any diff is a Task 1 defect and blocks the task.

- [ ] **Step 2: Verify cache version and field union**

```bash
set -euo pipefail
rg -n 'apiKeyAuthSnapshotVersion = 15|VideoRateIndependent|VideoRateMultiplier|VideoPrice480P|VideoPrice720P|VideoPrice1080P|AnthropicMixedTypeWeightEnabled|AnthropicSetupTokenPoolWeight|AnthropicAPIKeyPoolWeight' \
  backend/internal/service/api_key_auth_cache.go \
  backend/internal/service/api_key_auth_cache_impl.go \
  backend/internal/service/api_key_service_cache_test.go
```

Expected: all eight fields occur in snapshot type, write path, restore path, and round-trip test.

- [ ] **Step 3: Validate pricing JSON and model union**

```bash
set -euo pipefail
jq empty backend/resources/model-pricing/model_prices_and_context_window.json
rg -n 'claude-sonnet-5|claude-fable-5|gpt-5\.6|grok-4\.5' \
  backend/resources/model-pricing/model_prices_and_context_window.json \
  frontend/src/composables/useModelWhitelist.ts \
  frontend/src/composables/__tests__/useModelWhitelist.spec.ts
```

Expected: JSON parser exit `0`; all four model families have intended production and test coverage.

- [ ] **Step 4: Verify bulk account import and upstream route additions coexist**

```bash
set -euo pipefail
rg -n 'anthropicSessionBulkImport|sessionKeys|startBatchImport|180000|pool-health' \
  backend/internal/server/routes/admin.go \
  frontend/src/api/admin/accounts.ts \
  frontend/src/i18n/locales/en/admin/accounts.ts \
  frontend/src/i18n/locales/zh/admin/accounts.ts \
  frontend/src/router/index.ts

rg -n 'payment_enabled|risk_control_enabled|Grok|grok' \
  frontend/src/router/index.ts \
  frontend/src/router/__tests__/feature-access.spec.ts \
  frontend/src/composables/useGrokOAuth.ts \
  frontend/src/composables/__tests__/useGrokOAuth.spec.ts
```

Expected: sessionKey import API and translations remain, CRS timeout is 180 seconds, `/pool-health` remains routed, Grok OAuth copy is covered, and payment/risk-control routes retain feature guards.

- [ ] **Step 5: Run product contract tests and frontend build**

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go test -tags=unit ./internal/service \
  -run 'SnapshotRoundTrip|AnthropicMixedTypeWeights|Video|Pricing|GPT56|BatchImage|ClaudeTokenRefresher_CanRefresh|SchedulerSnapshot|HydratesSelectedAccount|StripOpenAIImageGenerationTools_StripsNamespaceFormats|OpenAIGatewayServiceRecordUsage_GPT56SeparatesCacheWriteForBillingAndStats|GPT56ExplicitZeroCacheWritePriceIsPreserved' \
  -count=1
cd ..

npm --prefix frontend run test:run -- \
  src/composables/__tests__/useModelWhitelist.spec.ts \
  src/composables/__tests__/useGrokOAuth.spec.ts \
  src/i18n/__tests__/anthropicSessionBulkImportLocales.spec.ts \
  src/i18n/__tests__/claudeCodeMimicryProfileLocales.spec.ts \
  src/router/__tests__/feature-access.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts

npm --prefix frontend run build
```

Expected: tests and build exit `0`. Record existing Vite/import/chunk warnings separately from regressions.

- [ ] **Step 6: Verify Docker/version anchors**

```bash
set -euo pipefail
test "$(tr -d '\r\n' < backend/cmd/server/VERSION)" = "0.1.150"
for file in Dockerfile deploy/Dockerfile; do
  rg -q '^ARG GOLANG_IMAGE=golang:1\.26\.5-alpine$' "$file"
  rg -q 'resolve-version\.sh' "$file"
  rg -q -- '-X main\.Version=' "$file"
  rg -q -- '-X main\.Commit=' "$file"
  rg -q -- '-trimpath' "$file"
done
```

Expected: VERSION test succeeds and each Docker definition independently contains Go 1.26.5, version resolution, Version/Commit ldflags, and `-trimpath`. This per-file loop prevents one Dockerfile from masking a missing anchor in the other.

- [ ] **Step 7: Complete the overlap coverage and product-contract report**

Copy the 45-path inventory from Task 2 into `.superpowers/sdd/upstream-merge-task-3-product-contract.md`. For every non-runtime path, record the fork-parent intent, upstream-parent intent, merged result, and verification evidence. Check that the Task 2 runtime rows plus Task 3 product rows cover all 45 paths exactly once. Then write PASS or FAIL with exact evidence; do not create an empty commit.

---

### Task 4: Validate Official Migrations and Disabled-by-Default Configuration

**Files:**
- Verify: `backend/migrations/159_batch_image_foundation.sql`
- Verify: both `backend/migrations/160_*.sql`
- Verify: `backend/migrations/161_*.sql` through `173_*.sql`
- Verify/test: `backend/migrations/auth_identity_payment_migrations_regression_test.go`
- Verify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`
- Write report: `.superpowers/sdd/upstream-merge-task-4-migrations.md`

**Interfaces:**
- Consumes: approved merged tree.
- Produces: proof that only new migrations were introduced, migration 173 matches runtime request type 4, and all batch-image defaults remain disabled.

- [ ] **Step 1: Prove old migration blobs were not rewritten**

```bash
set -euo pipefail
MERGE_COMMIT="$(git rev-list --merges --first-parent --max-count=1 HEAD)"
BASE_SHA="$(git rev-parse "${MERGE_COMMIT}^1")"
UPSTREAM_SHA="$(git rev-parse "${MERGE_COMMIT}^2")"
test "$UPSTREAM_SHA" = "6dd3274aafbc1a7a91304380fb3d7e50406841e0"
git diff --name-status "$BASE_SHA"..HEAD -- backend/migrations
test -z "$(git diff --name-only --diff-filter=MDR "$BASE_SHA"..HEAD -- 'backend/migrations/*.sql')"
git diff --exit-code "$UPSTREAM_SHA"..HEAD -- \
  backend/migrations/159_batch_image_foundation.sql \
  backend/migrations/160_add_user_frozen_balance.sql \
  backend/migrations/160_batch_image_provider_refs.sql \
  backend/migrations/161_batch_image_pricing_snapshot.sql \
  backend/migrations/162_add_group_batch_image_generation_gate.sql \
  backend/migrations/163_batch_image_default_discount_and_hold_ratio.sql \
  backend/migrations/164_batch_image_download_and_user_delete.sql \
  backend/migrations/165_hide_pre_upstream_batch_image_failures.sql \
  backend/migrations/166_batch_image_task_name.sql \
  backend/migrations/167_clear_auto_batch_image_task_names.sql \
  backend/migrations/168_restore_empty_batch_image_task_names.sql \
  backend/migrations/169_batch_image_parent_batch.sql \
  backend/migrations/170_add_grok_video_pricing_controls.sql \
  backend/migrations/171_allow_video_usage_without_image_size.sql \
  backend/migrations/172_video_per_second_billing_metadata.sql \
  backend/migrations/173_allow_cyber_blocked_usage_request_type.sql
```

Expected: the merge range adds only SQL migrations 170–173 and modifies `auth_identity_payment_migrations_regression_test.go`; the `MDR` assertion proves every SQL file already present at `BASE_SHA`, including fork migration 154, kept identical content and names; the upstream diff proves official migration blobs 159–173 are byte-for-byte identical to the frozen upstream parent.

- [ ] **Step 2: Verify the exact new migration inventory**

```bash
set -euo pipefail
rg --files backend/migrations | \
  rg 'backend/migrations/(159|160|161|162|163|164|165|166|167|168|169|170|171|172|173)_[^/]+\.sql$' | \
  sort

test "$(rg --files backend/migrations | rg 'backend/migrations/(159|160|161|162|163|164|165|166|167|168|169|170|171|172|173)_[^/]+\.sql$' | wc -l | tr -d ' ')" = "16"
```

Expected: the sorted inventory contains exactly 16 SQL files—159, two distinct 160 files, and 161 through 173. Migration 154 remains separately present as the fork mixed-pool migration.

- [ ] **Step 3: Verify migration 173 matches the runtime enum**

```bash
set -euo pipefail
rg -n 'RequestTypeCyberBlocked|request_type.*4' backend/internal backend/migrations
cd backend
GOTOOLCHAIN=go1.26.5 \
GOCACHE=/private/tmp/sub2api-go-build-cache \
GOPATH=/private/tmp/sub2api-go-path \
  go test ./migrations -run TestMigration173AllowsCyberBlockedUsageRequestType -count=1
cd ..
```

Expected: runtime enum value 4, SQL check `(0, 1, 2, 3, 4) NOT VALID`, and regression test PASS.

- [ ] **Step 4: Verify all batch-image defaults remain disabled**

```bash
set -euo pipefail
for key in enabled queue_enabled vertex_enabled; do
  rg -n -F "viper.SetDefault(\"batch_image.${key}\", false)" \
    backend/internal/config/config.go
done

cd backend
GOTOOLCHAIN=go1.26.5 \
GOCACHE=/private/tmp/sub2api-go-build-cache \
GOPATH=/private/tmp/sub2api-go-path \
  go test ./internal/config -run 'Test.*BatchImage|TestLoadConfig' -count=1
cd ..
```

Expected: three false defaults and passing config tests. Do not copy values from `deploy/docker-compose.dev.yml` into production compose files.

- [ ] **Step 5: Complete the migration report**

Record inventory, old-file diff result, migration test output, config defaults, and the production schema-lock warning in `.superpowers/sdd/upstream-merge-task-4-migrations.md`. Create no commit.

---

### Task 5: Run Full Regression, Build the Binary, and Request Final Review

**Files:**
- Verify: entire merged repository.
- Build artifact: `/private/tmp/sub2api-0.1.150-merge-server`
- Write report: `.superpowers/sdd/upstream-merge-task-5-final-verification.md`
- Review package: `.superpowers/sdd/upstream-merge-final-${BASE_SHORT}-${HEAD_SHORT}.diff`, with both shell variables defined by Step 5.

**Interfaces:**
- Consumes: Task 1 merge commit and PASS reports from Tasks 2–4.
- Produces: final test/build evidence, clean branch state, and an independent whole-branch review verdict.

- [ ] **Step 1: Run the full backend unit suite**

```bash
set -euo pipefail
cd backend
export GOTOOLCHAIN=go1.26.5
export GOCACHE=/private/tmp/sub2api-go-build-cache
export GOPATH=/private/tmp/sub2api-go-path
go test -tags=unit ./... -count=1
cd ..
```

Expected: exit `0`. When the sandbox rejects an `httptest` loopback bind, rerun this exact command outside the sandbox and record both outputs.

- [ ] **Step 2: Run the complete frontend test suite and production build**

```bash
set -euo pipefail
npm --prefix frontend run test:run
npm --prefix frontend run build
```

Expected: all test files pass and Vite build exits `0`.

- [ ] **Step 3: Compile the embedded production server to a temporary path**

```bash
set -euo pipefail
(
  set -euo pipefail
  cd backend
  BIN=/private/tmp/sub2api-0.1.150-merge-server
  rm -f "$BIN"
  trap 'rm -f "$BIN"' EXIT
  export GOTOOLCHAIN=go1.26.5
  export GOCACHE=/private/tmp/sub2api-go-build-cache
  export GOPATH=/private/tmp/sub2api-go-path

  go build -tags=embed \
    -trimpath \
    -ldflags='-s -w -X main.Version=0.1.150 -X main.Commit=merge-local -X main.BuildType=release' \
    -o "$BIN" \
    ./cmd/server
  "$BIN" --version
)
test ! -e /private/tmp/sub2api-0.1.150-merge-server
```

Expected: build exit `0`; version output contains `Sub2API 0.1.150` and commit `merge-local`; the temporary binary is removed after its output is recorded.

- [ ] **Step 4: Verify final Git and merge provenance**

```bash
set -euo pipefail
MERGE_COMMIT="$(git rev-list --merges --first-parent --max-count=1 HEAD)"
BASE_SHA="$(git rev-parse "${MERGE_COMMIT}^1")"
UPSTREAM_SHA="$(git rev-parse "${MERGE_COMMIT}^2")"
git diff --check
git diff --check "$BASE_SHA"..HEAD
git status --short --branch
test -z "$(git status --porcelain)"
test "$BASE_SHA" = "$(git rev-parse custom/prod)"
test "$UPSTREAM_SHA" = "6dd3274aafbc1a7a91304380fb3d7e50406841e0"
test "$(tr -d '\r\n' < backend/cmd/server/VERSION)" = "0.1.150"
```

Expected: clean named branch, the located merge commit has Task 0's base and frozen upstream parents even when bounded repair commits follow it, and VERSION check exits `0`.

- [ ] **Step 5: Generate the fixed final review package**

Run:

```bash
set -euo pipefail
MERGE_COMMIT="$(git rev-list --merges --first-parent --max-count=1 HEAD)"
BASE_SHA="$(git rev-parse "${MERGE_COMMIT}^1")"
BASE_SHORT="$(git rev-parse --short=8 "$BASE_SHA")"
HEAD_SHORT="$(git rev-parse --short=8 HEAD)"
REVIEW_PACKAGE=".superpowers/sdd/upstream-merge-final-${BASE_SHORT}-${HEAD_SHORT}.diff"
/Users/asenyu/.codex/plugins/cache/openai-curated-remote/superpowers/6.1.1/skills/subagent-driven-development/scripts/review-package \
  "$BASE_SHA" \
  HEAD \
  "$REVIEW_PACKAGE"
test -s "$REVIEW_PACKAGE"
printf '%s\n' "$REVIEW_PACKAGE"
```

Expected: the helper prints the same non-empty package path. After any repair commit, rerun this exact step so `HEAD_SHORT` produces a new package rather than reusing stale review evidence.

- [ ] **Step 6: Request an independent whole-branch review**

The fresh reviewer reads the design, this plan, all task reports, merge commit, and fixed diff package. Required output:

- strengths with file/symbol evidence;
- Critical, Important, and Minor findings separated;
- migration/config and production-risk gaps separated from code defects;
- `Ready: Yes/No`.

Critical/Important findings require one bounded repair task, focused tests, a new commit, regenerated review package, and re-review.

- [ ] **Step 7: Write the final verification report**

Record exact test counts, durations, build/version output, warnings, parent SHAs, review verdict, and worktree status in `.superpowers/sdd/upstream-merge-task-5-final-verification.md`.

- [ ] **Step 8: Hand off through the finishing-development-branch workflow**

After all gates pass, use `superpowers:finishing-a-development-branch` and present exactly:

1. Merge back to `custom/prod` locally.
2. Push and create a Pull Request.
3. Keep the branch as-is.
4. Discard this work.

Do not push or deploy without a new explicit user choice.

# Anthropic Mixed Type Weight Scheduling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in weighted scheduling between setup-token and api-key accounts inside an Anthropic group.

**Architecture:** Store group-level pool weights on `groups`, store api-key account weight on `accounts`, pass those fields through service/repository/DTO/auth-cache/scheduler-cache, and apply the policy only in Anthropic load-aware Layer 2 after sticky/model-routing checks. Defaults preserve existing production behavior.

**Tech Stack:** Go backend with Ent ORM and SQL migrations, Vue 3 + TypeScript frontend, Vitest for frontend tests, Go unit tests with build tag `unit`.

---

### Task 1: Backend Model, Persistence, and API Fields

**Files:**
- Modify: `backend/ent/schema/group.go`
- Modify: `backend/ent/schema/account.go`
- Create: `backend/migrations/154_anthropic_mixed_type_weight_scheduling.sql`
- Modify: `backend/internal/service/group.go`
- Modify: `backend/internal/service/account.go`
- Modify: `backend/internal/service/admin_service.go`
- Modify: `backend/internal/handler/admin/group_handler.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/repository/group_repo.go`
- Modify: `backend/internal/repository/account_repo.go`
- Modify: `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/service/api_key_auth_cache.go`
- Modify: `backend/internal/service/api_key_auth_cache_impl.go`
- Modify: `backend/internal/repository/scheduler_cache.go`
- Generated: `backend/ent/**`
- Test: `backend/internal/service/admin_service_group_test.go`
- Test: `backend/internal/repository/api_key_repo_messages_dispatch_unit_test.go`

- [ ] **Step 1: Write failing backend field tests**

Add tests that assert:

```go
// backend/internal/service/admin_service_group_test.go
func TestAdminService_CreateGroup_NormalizesAnthropicMixedTypeWeights(t *testing.T)
func TestAdminService_UpdateGroup_RejectsAllZeroAnthropicMixedTypeWeights(t *testing.T)
```

Add repository mapper assertions in `TestGroupEntityToService_PreservesMessagesDispatchModelConfig` or a new adjacent test:

```go
require.True(t, got.AnthropicMixedTypeWeightEnabled)
require.Equal(t, 100, got.AnthropicSetupTokenPoolWeight)
require.Equal(t, 25, got.AnthropicAPIKeyPoolWeight)
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go test -tags=unit ./internal/service -run 'TestAdminService_(CreateGroup_NormalizesAnthropicMixedTypeWeights|UpdateGroup_RejectsAllZeroAnthropicMixedTypeWeights)' -count=1
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go test -tags=unit ./internal/repository -run 'TestGroupEntityToService_.*AnthropicMixedTypeWeights' -count=1
```

Expected: FAIL because fields do not exist yet.

- [ ] **Step 3: Implement fields and persistence**

Add group fields:

```go
AnthropicMixedTypeWeightEnabled bool
AnthropicSetupTokenPoolWeight   int
AnthropicAPIKeyPoolWeight       int
```

Add account field:

```go
PoolWeight *int
```

Add account method:

```go
func (a *Account) EffectivePoolWeight() int {
    if a == nil || a.PoolWeight == nil {
        return 1
    }
    return *a.PoolWeight
}
```

Add SQL migration:

```sql
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS anthropic_mixed_type_weight_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS anthropic_setup_token_pool_weight INTEGER NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS anthropic_api_key_pool_weight INTEGER NOT NULL DEFAULT 0;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS pool_weight INTEGER NOT NULL DEFAULT 1;
```

Add validation:

```go
if enabled && setupWeight == 0 && apiKeyWeight == 0 {
    return errors.New("at least one anthropic mixed type pool weight must be > 0")
}
```

Add `PoolWeight` to create/update account requests and DTOs. Validate `pool_weight >= 0`; nil means unchanged/default.

- [ ] **Step 4: Generate Ent code**

Run:

```bash
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go generate ./ent
```

Expected: generated Ent files include the new fields and setters.

- [ ] **Step 5: Run tests and verify they pass**

Run the same commands from Step 2.

- [ ] **Step 6: Commit backend field plumbing**

Commit message:

```bash
git commit -m "feat: add mixed type scheduling fields"
```

### Task 2: Weighted Anthropic Scheduler

**Files:**
- Modify: `backend/internal/service/gateway_service.go`
- Test: `backend/internal/service/gateway_account_selection_test.go`
- Test: `backend/internal/service/gateway_multiplatform_test.go`

- [ ] **Step 1: Write failing scheduler helper tests**

Add tests for helper behavior:

```go
func TestMixedTypeSchedulingFiltersAPIKeyWhenPoolWeightZero(t *testing.T)
func TestSelectWeightedAPIKeyCandidateSkipsZeroPoolWeight(t *testing.T)
func TestSelectWeightedPoolChoiceUsesRelativeWeights(t *testing.T)
```

- [ ] **Step 2: Write failing integration-style selection tests**

Add tests in `gateway_multiplatform_test.go`:

```go
func TestSelectAccountWithLoadAwareness_MixedTypeWeight_APIKeyWeightZeroUsesSetupToken(t *testing.T)
func TestSelectAccountWithLoadAwareness_MixedTypeWeight_SetupUnavailableFallsBackToAPIKey(t *testing.T)
func TestSelectAccountWithLoadAwareness_MixedTypeWeight_StickyStillWins(t *testing.T)
```

- [ ] **Step 3: Run tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go test -tags=unit ./internal/service -run 'TestMixedTypeScheduling|TestSelectWeightedAPIKeyCandidate|TestSelectWeightedPoolChoice|TestSelectAccountWithLoadAwareness_MixedTypeWeight' -count=1
```

Expected: FAIL because helpers and scheduling branch do not exist yet.

- [ ] **Step 4: Implement weighted Layer 2 path**

Add helpers:

```go
func shouldUseAnthropicMixedTypeWeightScheduling(group *Group, platform string) bool
func filterAvailableForMixedTypePolicy(available []accountWithLoad, group *Group) []accountWithLoad
func selectMixedTypePool(setupWeight, apiKeyWeight int) string
func selectWeightedAPIKeyCandidate(accounts []accountWithLoad) *accountWithLoad
func selectDefaultLoadAwareCandidate(accounts []accountWithLoad, preferOAuth bool) *accountWithLoad
```

Behavior:

- Only active when `group.Platform == PlatformAnthropic` and group policy is enabled.
- Group pool weight `0` excludes that pool.
- Api-key account effective pool weight `0` excludes that account.
- Setup-token pool uses existing priority/load/LRU candidate selection.
- Api-key pool uses weighted selection among usable api-key candidates, adjusted by load.
- If chosen pool cannot acquire a slot, try the other eligible pool before falling through to existing wait-plan logic.
- Sticky-session and model routing layers remain before the new branch.

- [ ] **Step 5: Run scheduler tests and verify they pass**

Run the command from Step 3.

- [ ] **Step 6: Commit scheduler behavior**

Commit message:

```bash
git commit -m "feat: add Anthropic mixed type weighted scheduler"
```

### Task 3: Frontend Controls

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: `frontend/src/components/account/CreateAccountModal.vue`
- Modify: `frontend/src/components/account/EditAccountModal.vue`

- [ ] **Step 1: Write failing frontend type/form tests if local pattern exists**

Check for nearby tests:

```bash
rg -n "GroupsView|CreateAccountModal|EditAccountModal|pool_weight|model_routing_enabled" frontend/src -g '*.spec.ts'
```

If there is no practical existing component test pattern for these very large modal files, rely on `vue-tsc` and targeted manual DOM inspection after build. Do not add brittle shallow tests for the full modal stack.

- [ ] **Step 2: Add TypeScript fields**

Add to group types:

```ts
anthropic_mixed_type_weight_enabled: boolean
anthropic_setup_token_pool_weight: number
anthropic_api_key_pool_weight: number
```

Add to account create/update/response types:

```ts
pool_weight?: number | null
```

- [ ] **Step 3: Add group form controls**

In create/edit group modal, show for Anthropic platform:

```vue
<input v-model.number="createForm.anthropic_setup_token_pool_weight" type="number" min="0" class="input" />
<input v-model.number="createForm.anthropic_api_key_pool_weight" type="number" min="0" class="input" />
```

Add an enable toggle using the existing button/toggle visual style near model routing or account filtering settings.

- [ ] **Step 4: Add api-key account pool weight control**

Show only for `type === 'apikey'`:

```vue
<input v-model.number="form.pool_weight" type="number" min="0" class="input" />
```

Create default: `1`. Edit sync: `newAccount.pool_weight ?? 1`.

- [ ] **Step 5: Run frontend verification**

Run:

```bash
pnpm --dir frontend run typecheck
```

Expected: PASS.

- [ ] **Step 6: Commit frontend controls**

Commit message:

```bash
git commit -m "feat: add mixed type scheduling controls"
```

### Task 4: Final Verification

**Files:**
- All modified files

- [ ] **Step 1: Run targeted backend tests**

Run:

```bash
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go test -tags=unit ./internal/service -run 'TestAdminService_(CreateGroup_NormalizesAnthropicMixedTypeWeights|UpdateGroup_RejectsAllZeroAnthropicMixedTypeWeights)|TestMixedTypeScheduling|TestSelectWeightedAPIKeyCandidate|TestSelectWeightedPoolChoice|TestSelectAccountWithLoadAwareness_MixedTypeWeight' -count=1
```

- [ ] **Step 2: Run handler/admin tests touched by account payload changes**

Run:

```bash
GOCACHE=/tmp/sub2api-gocache GOMODCACHE=/tmp/sub2api-gomodcache go test -tags=unit ./internal/handler/admin -run 'Test.*Account|Test.*Group' -count=1
```

- [ ] **Step 3: Run frontend typecheck**

Run:

```bash
pnpm --dir frontend run typecheck
```

- [ ] **Step 4: Inspect diff**

Run:

```bash
git diff --stat
git diff --check
```

- [ ] **Step 5: Commit any remaining fixes**

If final verification required small fixes, commit them with:

```bash
git commit -m "fix: polish mixed type scheduling"
```

---

## Self-Review

- Spec coverage: group pool weights, api-key account weights, default disabled behavior, api-key weight zero, sticky preservation, fallback, admin controls, and rollout defaults are covered.
- Completion scan: no unfinished instructions remain.
- Type consistency: backend uses `AnthropicMixedTypeWeightEnabled`, `AnthropicSetupTokenPoolWeight`, `AnthropicAPIKeyPoolWeight`, and account `PoolWeight`; frontend/API JSON uses `anthropic_mixed_type_weight_enabled`, `anthropic_setup_token_pool_weight`, `anthropic_api_key_pool_weight`, and `pool_weight`.

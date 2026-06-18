# Anthropic SessionKey Bulk Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CPA-style bulk `sessionKey` import for Anthropic setup-token accounts in the Sub2API admin accounts page.

**Architecture:** Add an in-memory backend import job under admin accounts that reuses `OAuthService.CookieAuth` and `AdminService.CreateAccount/UpdateAccount`. Add a Vue modal on the existing accounts page that starts the job, polls progress, and displays progress/statistics while preserving existing Anthropic account settings as batch defaults.

**Tech Stack:** Go/Gin backend, Ent-backed service layer, Vue 3 + TypeScript frontend, Vitest frontend tests, Go unit tests.

---

### Task 1: Backend Job API And Core Tests

**Files:**
- Create: `backend/internal/handler/admin/account_anthropic_session_import.go`
- Create: `backend/internal/handler/admin/account_anthropic_session_import_test.go`
- Modify: `backend/internal/server/routes/admin.go`

- [ ] **Step 1: Write backend tests for request normalization and job lifecycle**

Add tests that create an `AccountHandler` with fake OAuth/admin dependencies where needed, then verify:

```go
func TestNormalizeAnthropicSessionImportKeys(t *testing.T) {
	keys, duplicates := normalizeAnthropicSessionImportKeys([]string{" a ", "b", "a", ""})
	require.Equal(t, []string{"a", "b"}, keys)
	require.Equal(t, 1, duplicates)
}

func TestAnthropicSessionImportJobSnapshotRedactsSecrets(t *testing.T) {
	job := newAnthropicSessionImportJobForTest()
	job.appendResult(anthropicSessionImportItem{
		SessionKeyHash: "abc123",
		Email: "user@example.com",
		Plan: "Max",
		ProxyLabel: "proxy-1",
		Action: "created",
	})
	body, err := json.Marshal(job.snapshot())
	require.NoError(t, err)
	require.NotContains(t, string(body), "sk-ant-sid")
	require.NotContains(t, string(body), "proxy-password")
	require.Contains(t, string(body), "user@example.com")
}
```

- [ ] **Step 2: Run tests and confirm they fail**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'TestNormalizeAnthropicSessionImportKeys|TestAnthropicSessionImportJobSnapshotRedactsSecrets' -count=1
```

Expected: fail because the new functions/types do not exist.

- [ ] **Step 3: Implement job DTOs, normalizers, and snapshot helpers**

Create `account_anthropic_session_import.go` with:

```go
const maxAnthropicSessionImportResults = 500

type AnthropicSessionImportStartRequest struct {
	SessionKeys []string `json:"session_keys"`
	GroupIDs []int64 `json:"group_ids"`
	FixedProxyID *int64 `json:"fixed_proxy_id"`
	ProxyMode string `json:"proxy_mode"`
	AccountConcurrency *int `json:"account_concurrency"`
	Priority *int `json:"priority"`
	RateMultiplier *float64 `json:"rate_multiplier"`
	LoadFactor *int `json:"load_factor"`
	Extra map[string]any `json:"extra"`
	CredentialsExtra map[string]any `json:"credentials_extra"`
	ExpiresAt *int64 `json:"expires_at"`
	AutoPauseOnExpired *bool `json:"auto_pause_on_expired"`
	UpdateExisting *bool `json:"update_existing"`
	ConfirmMixedChannelRisk *bool `json:"confirm_mixed_channel_risk"`
	JobConcurrency int `json:"job_concurrency"`
	DelayMinMS int `json:"delay_min_ms"`
	DelayMaxMS int `json:"delay_max_ms"`
}

type AnthropicSessionImportJobSnapshot struct {
	ID string `json:"id"`
	Status string `json:"status"`
	Total int `json:"total"`
	Processed int `json:"processed"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Failed int `json:"failed"`
	Duplicate int `json:"duplicate"`
	Rejected int `json:"rejected"`
	FailureReasons map[string]int `json:"failure_reasons,omitempty"`
	PlanCounts map[string]int `json:"plan_counts,omitempty"`
	ProxyCounts map[string]int `json:"proxy_counts,omitempty"`
	Items []anthropicSessionImportItem `json:"items,omitempty"`
}
```

Implement `normalizeAnthropicSessionImportKeys`, `shortAnthropicSessionKeyHash`, job locking, `snapshot`, and capped `appendResult`.

- [ ] **Step 4: Run focused backend tests**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'TestNormalizeAnthropicSessionImportKeys|TestAnthropicSessionImportJobSnapshotRedactsSecrets' -count=1
```

Expected: pass.

### Task 2: Backend Import Execution

**Files:**
- Modify: `backend/internal/handler/admin/account_anthropic_session_import.go`
- Modify: `backend/internal/handler/admin/account_anthropic_session_import_test.go`
- Modify: `backend/internal/server/routes/admin.go`

- [ ] **Step 1: Write tests for create, update, proxy assignment, and stats**

Add table tests using fake functions injected into the handler file:

```go
func TestAnthropicSessionImportCreatesSetupTokenAccount(t *testing.T) {
	// fake cookie auth returns email/account UUID and fake admin create captures input.
	// assert PlatformAnthropic, AccountTypeSetupToken, selected group IDs, selected proxy ID,
	// credentials["session_key"], and generated name "user@example.com Max".
}

func TestAnthropicSessionImportAutoProxyUsesActiveProxy(t *testing.T) {
	// fake proxy list contains active and inactive proxies.
	// assert only active proxy IDs are selected.
}

func TestAnthropicSessionImportUpdatesExistingAccount(t *testing.T) {
	// fake existing account index matches by email.
	// assert update path increments Updated and does not create duplicate account.
}
```

- [ ] **Step 2: Run tests and confirm they fail**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'TestAnthropicSessionImportCreatesSetupTokenAccount|TestAnthropicSessionImportAutoProxyUsesActiveProxy|TestAnthropicSessionImportUpdatesExistingAccount' -count=1
```

Expected: fail until execution logic exists.

- [ ] **Step 3: Implement handlers and execution loop**

Register routes:

```go
accounts.POST("/import/anthropic-session", h.Admin.Account.StartAnthropicSessionImport)
accounts.GET("/import/anthropic-session/:id", h.Admin.Account.GetAnthropicSessionImport)
accounts.POST("/import/anthropic-session/:id/cancel", h.Admin.Account.CancelAnthropicSessionImport)
```

Implement:

- one running job guard with `409`
- worker pool bounded by `JobConcurrency`, default 5, max 20
- optional delay between items
- `OAuthService.CookieAuth` call with `Scope: "inference"`
- account creation/update through existing admin service
- account name generation from email and plan
- active proxy selection from `GetAllProxiesWithAccountCount`
- stable failure reason classification

- [ ] **Step 4: Run backend admin handler tests**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'AnthropicSessionImport' -count=1
```

Expected: pass.

### Task 3: Frontend API Types

**Files:**
- Modify: `frontend/src/api/admin/accounts.ts`
- Modify: `frontend/src/types/index.ts`

- [ ] **Step 1: Add frontend API type tests if existing API tests cover account APIs**

If no nearby API type test exists, rely on TypeScript build for this task.

- [ ] **Step 2: Add request/response types and API functions**

Add types:

```ts
export interface AnthropicSessionImportStartRequest {
  session_keys: string[]
  group_ids?: number[]
  fixed_proxy_id?: number | null
  proxy_mode?: 'auto' | 'fixed'
  account_concurrency?: number
  priority?: number
  rate_multiplier?: number
  load_factor?: number | null
  extra?: Record<string, unknown>
  credentials_extra?: Record<string, unknown>
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  update_existing?: boolean
  confirm_mixed_channel_risk?: boolean
  job_concurrency?: number
  delay_min_ms?: number
  delay_max_ms?: number
}
```

Add API methods:

```ts
export async function startAnthropicSessionImport(payload: AnthropicSessionImportStartRequest) {
  const { data } = await apiClient.post('/admin/accounts/import/anthropic-session', payload)
  return data
}
```

- [ ] **Step 3: Run frontend type check**

Run:

```bash
cd frontend
npm run type-check
```

Expected: pass.

### Task 4: Frontend Modal Integration

**Files:**
- Create: `frontend/src/components/admin/account/AnthropicSessionImportModal.vue`
- Modify: `frontend/src/views/admin/AccountsView.vue`
- Test: `frontend/src/views/admin/__tests__/AccountsView.anthropicSessionImport.spec.ts`

- [ ] **Step 1: Write modal test**

Test that the modal:

```ts
it('starts a bulk session import with selected groups and account defaults', async () => {
  // mount AccountsView or modal with mocked adminAPI.accounts.startAnthropicSessionImport
  // paste two session keys
  // select group ccmax
  // click start
  // expect payload.session_keys length to be 2
  // expect payload.proxy_mode to be "auto"
  // expect payload.group_ids to include the selected group id
})
```

- [ ] **Step 2: Run modal test and confirm it fails**

Run:

```bash
cd frontend
npm run test -- AccountsView.anthropicSessionImport
```

Expected: fail until component exists.

- [ ] **Step 3: Implement modal UI**

Implement a modal with:

- textarea
- group selector
- proxy mode selector, default auto
- fixed proxy selector shown only in fixed mode
- import concurrency and delay inputs
- account defaults copied from existing create account state patterns
- start/cancel actions
- progress bar and stats
- recent result list

- [ ] **Step 4: Integrate modal into AccountsView**

Add a `批量 sessionKey 导入` button on the accounts page and refresh accounts/proxies when a job completes.

- [ ] **Step 5: Run frontend tests and type check**

Run:

```bash
cd frontend
npm run type-check
npm run test -- AccountsView.anthropicSessionImport
```

Expected: pass.

### Task 5: Final Verification

**Files:**
- Verify all modified backend/frontend files.

- [ ] **Step 1: Format Go code**

Run:

```bash
cd backend
gofmt -w internal/handler/admin/account_anthropic_session_import.go internal/handler/admin/account_anthropic_session_import_test.go internal/server/routes/admin.go
```

- [ ] **Step 2: Run focused backend tests**

Run:

```bash
cd backend
go test ./internal/handler/admin -run 'AnthropicSessionImport' -count=1
```

Expected: pass.

- [ ] **Step 3: Run frontend checks**

Run:

```bash
cd frontend
npm run type-check
npm run test -- AccountsView.anthropicSessionImport
```

Expected: pass.

- [ ] **Step 4: Check worktree**

Run:

```bash
git status --short
```

Expected: only intentional source, test, and docs changes.

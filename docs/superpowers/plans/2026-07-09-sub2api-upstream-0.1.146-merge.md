# Sub2API Upstream 0.1.146 Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge official `upstream/main` at Sub2API `0.1.146` into the fork while preserving fork-specific production behavior and stopping before deployment.

**Architecture:** Work in an isolated Git worktree on `merge/upstream-0.1.146`. Use Git's normal three-way merge, resolve conflicts line-by-line, and keep official refactors while re-applying fork behavior in the new split-file layout. Verify with focused backend and frontend tests before any push or deployment decision.

**Tech Stack:** Go backend with Ent migrations, Vue/Vite frontend with pnpm, Docker deploy files.

## Global Constraints

- Do not deploy or SSH to production in this task.
- Do not use `git checkout --ours`, `git checkout --theirs`, `git reset --hard`, or other broad overwrite shortcuts for conflicts.
- Preserve fork behavior for Claude Code mimicry, Anthropic OAuth forwarding, content safety decisions, prompt placeholder handling, pre-slot stream behavior, Anthropic mixed-type scheduling, and deployment image/version semantics.
- Use `backend/cmd/server/VERSION` from `upstream/main` as the version source, expected value `0.1.146`.
- Keep fork migration `backend/migrations/154_anthropic_mixed_type_weight_scheduling.sql` and add official migrations `159` through `169`.
- Keep production deployment out of scope; deployment requires a separate explicit authorization.

---

### Task 1: Establish Baseline and Merge Target

**Files:**
- Read: `backend/cmd/server/VERSION`
- Read: `frontend/package.json`
- Read: `backend/go.mod`
- Modify: none
- Test: focused baseline commands below

**Interfaces:**
- Consumes: current branch `merge/upstream-0.1.146`, remote ref `upstream/main`
- Produces: verified starting point for merge work

- [ ] **Step 1: Verify clean worktree**

Run: `git status --short --branch`
Expected: branch is `merge/upstream-0.1.146` and status is clean.

- [ ] **Step 2: Verify target version**

Run: `git show upstream/main:backend/cmd/server/VERSION`
Expected: `0.1.146`

- [ ] **Step 3: Run backend baseline smoke tests**

Run: `cd backend && GOCACHE=/private/tmp/sub2api-go-build-cache go test ./internal/service -run 'TestContentSafety|TestClaude|TestGateway|TestSetting' -count=1`
Expected: tests pass, or failures are recorded as pre-merge baseline failures before continuing.

- [ ] **Step 4: Run frontend baseline smoke tests**

Run: `cd frontend && pnpm test --run src/views/admin/__tests__/SettingsView.spec.ts src/composables/__tests__/useModelWhitelist.spec.ts`
Expected: tests pass, or failures are recorded as pre-merge baseline failures before continuing.

### Task 2: Merge Upstream and Resolve Backend Conflicts

**Files:**
- Modify: `backend/ent/group.go`
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Modify: `backend/internal/service/admin_service.go`
- Modify: `backend/internal/service/antigravity_gateway_service.go`
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/migrations/154_anthropic_mixed_type_weight_scheduling.sql`
- Modify: `deploy/Dockerfile`
- Test: backend commands below

**Interfaces:**
- Consumes: upstream split-file backend layout and fork-specific backend behavior
- Produces: compilable backend with fork scheduling, gateway, content safety, and deploy semantics preserved

- [ ] **Step 1: Start the merge**

Run: `git merge --no-ff upstream/main`
Expected: Git reports conflicts in the known conflict set.

- [ ] **Step 2: Resolve Ent and migration conflicts**

Keep official generated Ent additions, keep fork fields `pool_weight`, `anthropic_mixed_type_weight_enabled`, `anthropic_setup_token_pool_weight`, and `anthropic_api_key_pool_weight`, and keep `backend/migrations/154_anthropic_mixed_type_weight_scheduling.sql`.

- [ ] **Step 3: Resolve service and handler conflicts**

Preserve upstream file splits and move fork logic to the current upstream functions instead of restoring old monolithic files. Confirm fork symbols remain searchable with:

Run: `rg 'ContentSafety|AnthropicMixedType|pool_weight|Claude Code|prompt placeholders|pre-slot|OAuth' backend/internal backend/ent backend/migrations`
Expected: fork behavior remains present in the post-merge file layout.

- [ ] **Step 4: Resolve deploy Dockerfile conflict**

Keep upstream dependency and build updates while preserving fork deploy behavior: application version comes from `backend/cmd/server/VERSION`, image tag is handled outside source by deployment scripts, and production deployment is not changed here.

- [ ] **Step 5: Run backend verification**

Run: `cd backend && GOCACHE=/private/tmp/sub2api-go-build-cache go test ./internal/service ./internal/handler ./internal/repository -count=1`
Expected: PASS.

### Task 3: Resolve Frontend and i18n Conflicts

**Files:**
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en/**`
- Modify: `frontend/src/i18n/locales/zh/**`
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Test: frontend commands below

**Interfaces:**
- Consumes: upstream split i18n modules and fork admin settings/group controls
- Produces: frontend build and tests with fork admin controls preserved

- [ ] **Step 1: Resolve i18n modify/delete conflicts**

Adopt upstream split i18n module structure and port fork-only translation keys from old `en.ts` and `zh.ts` into the appropriate split modules.

- [ ] **Step 2: Resolve admin view conflicts**

Keep upstream admin UI changes while preserving fork settings for content safety, Claude Code mimicry, Anthropic scheduling, and prompt placeholder handling.

- [ ] **Step 3: Run frontend verification**

Run: `cd frontend && pnpm test --run src/views/admin/__tests__/SettingsView.spec.ts src/views/admin/__tests__/groupsImagePricing.spec.ts src/composables/__tests__/useModelWhitelist.spec.ts`
Expected: PASS.

- [ ] **Step 4: Run frontend build**

Run: `cd frontend && pnpm build`
Expected: PASS.

### Task 4: Final Merge Validation

**Files:**
- Read: all changed files from `git diff --name-status HEAD`
- Modify: merge commit only
- Test: full status and targeted version checks below

**Interfaces:**
- Consumes: resolved backend/frontend merge
- Produces: local committed merge branch ready for review, not deployment

- [ ] **Step 1: Check conflict markers and whitespace**

Run: `rg '<<<<<<<|=======|>>>>>>>' .`
Expected: no conflict markers.

Run: `git diff --check`
Expected: no whitespace errors.

- [ ] **Step 2: Check versions and migrations**

Run: `cat backend/cmd/server/VERSION`
Expected: `0.1.146`

Run: `test -f backend/migrations/154_anthropic_mixed_type_weight_scheduling.sql && test -f backend/migrations/169_batch_image_parent_batch.sql`
Expected: both files exist.

- [ ] **Step 3: Commit merge result**

Run: `git status --short`
Expected: only intended merge changes are staged or ready to stage.

Run: `git commit`
Expected: merge commit records `upstream/main` into `merge/upstream-0.1.146`.

- [ ] **Step 4: Report result**

Report commit hash, tests run, known residual risk, and explicitly state that no production deployment was performed.

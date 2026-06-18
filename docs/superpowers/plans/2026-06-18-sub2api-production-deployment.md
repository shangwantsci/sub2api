# Sub2API Production Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the custom `shangwantsci/sub2api` fork as a repeatable Docker image and deploy it to production with only the `sub2api` application container restarted.

**Architecture:** GitHub remains the source of truth. The fork publishes versioned GHCR images such as `ghcr.io/shangwantsci/sub2api:0.1.137-lumos.1`; the server pulls that immutable tag and runs `docker compose up -d --no-deps sub2api`, leaving PostgreSQL, Redis, Nginx, and the other two production projects untouched. Rollback is just changing the image tag back to the previous known-good tag and recreating only the `sub2api` container.

**Tech Stack:** Git, GitHub Actions, GHCR, Docker, Docker Compose, Go backend, Vue frontend, existing Sub2API `release.yml`.

---

### Task 1: One-Time Repository Release Setup

**Files:**
- Read: `.github/workflows/release.yml`
- Read: `.goreleaser.simple.yaml`
- No code changes expected

- [ ] **Step 1: Confirm remotes**

Run:

```bash
git remote -v
```

Expected:

```text
origin   https://github.com/shangwantsci/sub2api.git (fetch)
origin   https://github.com/shangwantsci/sub2api.git (push)
upstream https://github.com/Wei-Shaw/sub2api.git (fetch)
upstream DISABLED (push)
```

- [ ] **Step 2: Configure simple release mode once**

Run:

```bash
gh variable set SIMPLE_RELEASE --body true --repo shangwantsci/sub2api
```

Expected: GitHub repository variable `SIMPLE_RELEASE=true`.

Why: tag pushes then build only the x86_64 GHCR image, which is enough for the current production server and is faster than a full multi-arch release.

- [ ] **Step 3: Confirm GHCR package visibility**

Open:

```text
https://github.com/shangwantsci/sub2api/pkgs/container/sub2api
```

Expected: package is public, or production server has a GHCR login token.

If the package is private, log in on the server once:

```bash
docker login ghcr.io -u shangwantsci
```

Use a GitHub personal access token with package read permission.

### Task 2: Commit and Push Current Work

**Files:**
- Modify already staged later: backend cache fix files
- Modify already staged later: batch import files

- [ ] **Step 1: Re-run local verification**

Run:

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork/backend
GOCACHE=/private/tmp/sub2api-gocache GOMODCACHE=/private/tmp/sub2api-gomodcache go test ./...
```

Expected: all backend packages pass.

Run:

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork
git diff --check
```

Expected: no output and exit code 0.

- [ ] **Step 2: Commit the bulk import feature**

Run:

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork
git add \
  backend/internal/handler/admin/account_handler.go \
  backend/internal/server/routes/admin.go \
  backend/internal/handler/admin/account_anthropic_session_import.go \
  backend/internal/handler/admin/account_anthropic_session_import_test.go \
  frontend/src/api/admin/accounts.ts \
  frontend/src/components/account/CreateAccountModal.vue \
  frontend/src/types/index.ts
git commit -m "feat: add Anthropic session key bulk import"
```

Expected: one commit containing the UI/API/backend import workflow.

- [ ] **Step 3: Commit the cache fix**

Run:

```bash
cd /Users/asenyu/Desktop/中转站/claude号池项目/sub2api-fork
git add \
  backend/internal/service/gateway_service.go \
  backend/internal/service/gateway_prompt_test.go \
  backend/internal/service/gateway_anthropic_apikey_passthrough_test.go
git commit -m "fix: preserve Anthropic prompt cache controls"
```

Expected: one focused commit for the customer cache issue.

- [ ] **Step 4: Push the branch**

Run:

```bash
git push -u origin feature/anthropic-session-bulk-import
```

Expected: branch is available on GitHub.

### Task 3: Create a Versioned Release Image

**Files:**
- Read: `backend/cmd/server/VERSION`
- No code changes expected

- [ ] **Step 1: Choose immutable release tag**

Use the current base version plus a custom suffix:

```text
v0.1.137-lumos.1
```

Rule for future releases:

```text
v<official-version>-lumos.<n>
```

Examples:

```text
v0.1.137-lumos.2
v0.1.138-lumos.1
```

- [ ] **Step 2: Create annotated tag**

Run:

```bash
git tag -a v0.1.137-lumos.1 -m "Lumos production release v0.1.137-lumos.1

- Add Anthropic setup-token bulk import
- Preserve Anthropic prompt cache_control during OAuth/SetupToken mimicry
- Auto include extended-cache-ttl beta when ttl=1h is present"
```

Expected: local annotated tag exists.

- [ ] **Step 3: Push tag**

Run:

```bash
git push origin v0.1.137-lumos.1
```

Expected: GitHub Actions `Release` workflow starts automatically.

- [ ] **Step 4: Wait for release workflow**

Run:

```bash
gh run list --workflow Release --repo shangwantsci/sub2api --limit 5
```

Then watch the latest run:

```bash
gh run watch --repo shangwantsci/sub2api
```

Expected: release workflow succeeds and pushes:

```text
ghcr.io/shangwantsci/sub2api:0.1.137-lumos.1
ghcr.io/shangwantsci/sub2api:latest
```

### Task 4: One-Time Production Compose Standardization

**Files on server:**
- Read: existing Sub2API compose file
- Modify: only the Sub2API deployment compose file, not Nginx and not the other two projects

- [ ] **Step 1: Read-only production audit**

Run on server:

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker inspect sub2api --format '{{.Config.Image}}'
docker inspect sub2api --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'
```

Expected: identify current Sub2API image tag and data mounts.

- [ ] **Step 2: Locate compose directory**

Run on server:

```bash
find /root /opt /srv -maxdepth 4 -name 'docker-compose*.yml' -o -name 'compose*.yml'
```

Expected: locate the compose file that owns the `sub2api` container.

- [ ] **Step 3: Back up compose and env files**

Run inside the Sub2API deploy directory:

```bash
mkdir -p backups
cp docker-compose.yml "backups/docker-compose.yml.$(date +%Y%m%d-%H%M%S)"
cp .env "backups/.env.$(date +%Y%m%d-%H%M%S)"
```

Expected: reversible text backups.

- [ ] **Step 4: Change image to a variable**

Edit only the `sub2api` service image line:

```yaml
services:
  sub2api:
    image: ${SUB2API_IMAGE:-ghcr.io/shangwantsci/sub2api:0.1.137-lumos.1}
```

Add to `.env`:

```dotenv
SUB2API_IMAGE=ghcr.io/shangwantsci/sub2api:0.1.137-lumos.1
```

Expected: future upgrades only edit `.env`, not compose structure.

### Task 5: Low-Impact Production Deploy

**Files on server:**
- Modify: `.env` image tag only
- No database, Redis, or Nginx changes

- [ ] **Step 1: Pull new image before restart**

Run in the Sub2API deploy directory:

```bash
docker compose pull sub2api
```

Expected: image download completes while the old container is still serving traffic.

- [ ] **Step 2: Record rollback image**

Run:

```bash
docker inspect sub2api --format '{{.Config.Image}}' | tee backups/previous-sub2api-image.$(date +%Y%m%d-%H%M%S).txt
```

Expected: previous image tag is saved.

- [ ] **Step 3: Recreate only the app container**

Run:

```bash
docker compose up -d --no-deps sub2api
```

Expected: only `sub2api` is recreated. PostgreSQL, Redis, Nginx, and the other two projects keep running.

- [ ] **Step 4: Health check**

Run:

```bash
docker compose ps sub2api
docker compose logs --tail=120 sub2api
curl -fsS http://127.0.0.1:8080/health
curl -fsS https://lumos7.cc/health
```

Expected: container is healthy and both local/domain health checks return success.

- [ ] **Step 5: Customer cache regression smoke test**

Run a small 4-request smoke test first:

```bash
# 1 Haiku 5m, 1 Haiku 1h, 1 Opus 5m, 1 Opus 1h
# Expected:
# - 5m requests produce cache_creation.ephemeral_5m_input_tokens > 0
# - 1h requests produce cache_creation.ephemeral_1h_input_tokens > 0
```

Then run the full 20-request customer matrix only after the smoke test passes.

### Task 6: Rollback Procedure

**Files on server:**
- Modify: `.env` image tag only

- [ ] **Step 1: Restore previous image tag**

Edit `.env`:

```dotenv
SUB2API_IMAGE=<previous image from backups/previous-sub2api-image.*.txt>
```

- [ ] **Step 2: Pull previous image if needed**

Run:

```bash
docker compose pull sub2api || true
```

- [ ] **Step 3: Recreate only app container**

Run:

```bash
docker compose up -d --no-deps sub2api
curl -fsS https://lumos7.cc/health
```

Expected: service returns to previous version without touching database or Redis.

### Task 7: Standard Future Update Workflow

**Files:**
- Modify only files required by each future feature/fix

- [ ] **Step 1: Sync official upstream**

Run:

```bash
git fetch upstream
git fetch origin
git switch main
git pull --ff-only origin main
git merge --ff-only upstream/main || git merge upstream/main
```

Expected: local main contains official updates plus fork history.

- [ ] **Step 2: Create a feature branch**

Run:

```bash
git switch -c feature/<short-topic>
```

- [ ] **Step 3: Implement with tests**

Run before release:

```bash
cd backend
GOCACHE=/private/tmp/sub2api-gocache GOMODCACHE=/private/tmp/sub2api-gomodcache go test ./...
cd ..
git diff --check
```

- [ ] **Step 4: Commit, push, tag, release**

Run:

```bash
git push -u origin feature/<short-topic>
git tag -a v<official-version>-lumos.<n> -m "Lumos production release v<official-version>-lumos.<n>"
git push origin v<official-version>-lumos.<n>
```

Expected: GitHub Actions builds the image; production deploy repeats Task 5.

---

## Self-Review

Spec coverage:
- Commit and push to GitHub branch: Task 2.
- Repeatable production deployment: Tasks 3, 4, 5, 7.
- Minimal impact to running services: Task 5 uses `docker compose up -d --no-deps sub2api`, leaving PostgreSQL, Redis, Nginx, and other projects untouched.
- Fast future updates: immutable GHCR tags and `.env` image switch make the server update a pull plus one container recreate.
- Rollback: Task 6.

Placeholder scan:
- No TBD/TODO placeholders remain.

Operational boundary:
- This plan intentionally avoids database migration commands unless the app runs its existing auto-migrations on startup.
- This plan intentionally avoids editing Nginx/Caddy/other projects unless the production audit proves Sub2API is currently deployed differently.

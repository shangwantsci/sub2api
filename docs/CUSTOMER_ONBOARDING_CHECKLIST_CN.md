# 客户交付检查单（每个客户复制一份）

> 照着从上到下走即可。原理、边界、故障排查见
> [`CUSTOMER_DEPLOYMENT_LICENSE_CN.md`](CUSTOMER_DEPLOYMENT_LICENSE_CN.md)。
>
> 本单基于 2026-08-04 首个客户 yihang 的真实交付过程整理，每个「⚠」都是当时踩过的坑。

```text
客户名称        ______________    customer_id  ______________
服务器 IP       ______________    SSH 端口/用户 ______________
对外访问方式    □ 仅本机  □ IP+HTTP  □ 域名+HTTPS：______________
开放 Chrome Cookie 授权？   □ 否（默认）  □ 是
交付日期        ______________
```

---

## A 段 · 不需要客户服务器（提前做完）

### A1 确认授权中心健康

```bash
curl -sS https://license.lumos7.cc/health
```

期望 `{"status":"ok"}`。管理台：<https://license.lumos7.cc/console>

### A2 构建客户镜像

```bash
gh workflow run release.yml --repo shangwantsci/sub2api --ref custom/prod \
  -f tag="v$(tr -d '\r\n' < backend/cmd/server/VERSION)" \
  -f custom_image_only=true -f source_ref=custom/prod \
  -f customer_id=<客户ID> -f simple_release=true

gh run watch --repo shangwantsci/sub2api
```

产出两个 tag，**交付用不可变的那个**（带 commit 后缀）：

```text
ghcr.io/shangwantsci/sub2api-customer-<客户ID>:<VERSION>
ghcr.io/shangwantsci/sub2api-customer-<客户ID>:<VERSION>-<COMMIT>   ← 用这个
```

### A3 ⚠ 把 package 改成 Private

**新 package 默认是 public**（主仓是 public fork，Dockerfile 的
`org.opencontainers.image.source` 指向上游公开仓库，GHCR 继承了可见性）。
GitHub 没有改可见性的 REST API，**只能在网页操作**：

```text
https://github.com/users/shangwantsci/packages/container/sub2api-customer-<客户ID>/settings
→ Danger Zone → Change visibility → Private
```

改完必须复验，匿名 pull 应当失败：

```bash
docker logout ghcr.io 2>/dev/null
docker pull ghcr.io/shangwantsci/sub2api-customer-<客户ID>:<TAG>
```

- [ ] package 已确认 Private
- [ ] 匿名 pull 已验证失败

### A4 在管理台新建客户并生成激活码

<https://license.lumos7.cc/console> → 「新建客户并生成激活码」

- **功能开关**：不勾「Chrome Cookie 授权」= 客户看不到也用不了（后端 403）
- 激活码**只显示一次**，立刻复制

- [ ] 激活码已保存：`sub2_____________________`

### A5 生成交付文件

```bash
./deploy/new-customer-env.sh <客户ID> <完整镜像tag> <激活码>
```

生成 `out-<客户ID>/`，含 `.env`、两个 compose、`credentials.txt`（管理员账号密码）。

⚠ 脚本已自动处理 **`TOTP_ENCRYPTION_KEY` 必须是 64 位十六进制**——手工写字母数字混合会让容器无限重启，日志报 `invalid totp encryption key: encoding/hex: invalid byte`。

### A6 建只读 PAT 交给客户（可选）

只授权这一个 package。如果由你 SSH 上去部署，可以跳过——用你自己的凭证 pull 完后
`docker logout` 即可，客户服务器不留凭证。

---

## B 段 · 客户服务器上

### B1 连通性预检（决定成败，先做）

```bash
for u in https://license.lumos7.cc/health https://ghcr.io/v2/ https://api.anthropic.com; do
  printf "%-40s %s\n" "$u" "$(curl -sS -o /dev/null -m 12 -w '%{http_code}' "$u")"
done
```

期望：授权中心 **200**、GHCR **401**（需认证属正常）、Anthropic **405**（GET 不允许属正常）。
**授权中心不通就别往下走**，先让客户放开出站。

### B2 检查机器身份

```bash
ls -l /etc/machine-id && nproc && free -h | head -2 && df -h /
```

`/etc/machine-id` 必须存在，否则机器绑定失效。建议规格 ≥2C4G / 40G。

### B3 装 Docker（若未装）

```bash
apt-get update -qq && apt-get install -y -qq ca-certificates curl
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
apt-get update -qq && apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### B4 上传并启动

```bash
# 本地
ssh <客户> 'mkdir -p /opt/sub2api/{data,postgres_data,redis_data}'
scp out-<客户ID>/{docker-compose.local.yml,docker-compose.customer.yml,.env} <客户>:/opt/sub2api/

# 客户服务器
cd /opt/sub2api && chmod 600 .env
docker login ghcr.io
docker compose -f docker-compose.local.yml -f docker-compose.customer.yml config | grep -E "image:|DEPLOYMENT_LICENSE_(ENABLED|SERVER_URL)|UPDATE_DISABLED"
docker compose -f docker-compose.local.yml -f docker-compose.customer.yml up -d
```

**只传这三个文件**，不要 clone 仓库、不要放整个 `deploy/`。

---

## C 段 · 验收（全部通过才算交付）

### C1 容器与授权

```bash
docker compose -f docker-compose.local.yml -f docker-compose.customer.yml ps
docker logs sub2api 2>&1 | grep -iE "deployment_license|calibration"
```

- [ ] 三个容器都 healthy
- [ ] 有 `deployment_license.renewed`
- [ ] 有 `calibration_profile_applied`（授权中心已发布 profile 时）
- [ ] **没有** `no host-provided machine signals` 警告

### C2 交付目录干净

```bash
ls -la /opt/sub2api/ && ls /opt/sub2api/*.go 2>/dev/null; test -d /opt/sub2api/.git && echo "有 .git（不应该）"
```

- [ ] 无源码、无 `.git`

### C3 授权中心已登记

<https://license.lumos7.cc/console> → 「客户服务器实例」

- [ ] 实例已出现，状态「正常」
- [ ] 「已开功能」与预期一致（不该有 Chrome Cookie 就不该显示）

### C4 功能开关生效

登录客户后台 → 账号管理 → 添加账号 → 第二步「Claude 账号授权」

- [ ] 只有「手动授权」「Cookie 自动授权」两个选项（未开 Chrome Cookie 时）

后端也应拒绝：

```bash
curl -sS -o /dev/null -w "%{http_code}\n" -X POST http://<IP>/api/v1/admin/accounts/chrome-cookie-auth \
  -H "Authorization: Bearer <管理员JWT>" -H 'Content-Type: application/json' -d '{"code":"x"}'
```

- [ ] 返回 **403**

### C5 端到端跑通

先在后台加号、建分组、建 API Key，然后：

```bash
curl -sS -X POST http://<IP>/v1/messages \
  -H "Content-Type: application/json" -H "x-api-key: <APIKEY>" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"hi"}]}'
```

- [ ] 返回正常 message

⚠ **测试请求不要连发**。3 个 OAuth 号扛不住连续压测，会触发 Anthropic 429，
随后账号被临时排除、所有请求变成 `no available accounts`，冷却几分钟才恢复——
这不是故障，是限流保护。**测 1~2 次就够**。

---

## D 段 · 交付给客户的说明

```text
后台地址    http://<IP>          （见 credentials.txt 里的管理员账号）
API 地址    http://<IP>/v1/messages          Claude 原生格式
            http://<IP>/v1/chat/completions  OpenAI 兼容格式

Claude Code 配置：
  export ANTHROPIC_BASE_URL=http://<IP>
  export ANTHROPIC_AUTH_TOKEN=<后台创建的 API Key>
```

⚠ **必须告知客户的一条**：调用 **haiku** 时，`max_tokens` 要么不传、要么 ≥32001。
网关为伪装真实 Claude Code 会注入 `thinking.budget_tokens=31999`，客户端若传
`max_tokens: 1024/4096` 会被 Anthropic 拒绝（400 `max_tokens must be greater than
thinking.budget_tokens`）。sonnet / opus / fable 用 `adaptive` 类型不带 budget，**不受影响**。

首次登录后台会要求接受「部署与运营合规承诺」，需由客户自己点，不点则所有管理接口返回
`ADMIN_COMPLIANCE_ACK_REQUIRED`。

---

## E 段 · 收尾

- [ ] 删除本地 `out-<客户ID>/`（含明文密钥）
- [ ] 管理员密码交付后提醒客户修改
- [ ] 管理员邮箱是 `admin@<客户ID>.local` 假域名，收不到邮件，建议客户改成真实邮箱
- [ ] 若走 HTTP 明文，提醒客户 API Key 有被截获风险，建议后续加域名 + HTTPS
- [ ] 在 `FORK_DEPLOY_RUNBOOK_CN.md` 追加一行交付记录

---

## 快速故障对照

| 现象 | 原因 | 处理 |
|---|---|---|
| 容器无限重启，`invalid totp encryption key` | TOTP 不是 hex | 用 `token_hex(32)` 重新生成 |
| 启动失败，`license public key` 相关 | 仓库变量 `DEPLOYMENT_LICENSE_PUBLIC_KEY` 缺失或与线上私钥不符 | 核对管理台公钥 |
| `deployment license has no host-provided machine signals` | `/etc/machine-id` 未挂载 | 检查 compose 的 volumes |
| 400 `max_tokens must be greater than thinking.budget_tokens` | 客户端 max_tokens 太小 | 不传或 ≥32001；换 sonnet/opus |
| 403 `deployment_capability_disabled` | 该能力未授予 | 管理台勾选，客户下次续租生效 |
| 503 `no available accounts` 且此前有 429 | 限流后账号被临时排除 | 停手等几分钟自愈 |
| 管理接口 `ADMIN_COMPLIANCE_ACK_REQUIRED` | 未接受合规承诺 | 客户在后台点确认 |

#!/usr/bin/env bash
# 生成一个客户的 .env。所有密钥随机，格式经过校验。
#
# 用法：
#   ./deploy/new-customer-env.sh <customer_id> <image_tag> <activation_code> [license_url] [bind_port]
#
# 例：
#   ./deploy/new-customer-env.sh yihang \
#       ghcr.io/shangwantsci/sub2api-customer-yihang:0.1.156-74ccab74 \
#       sub2_xxxxx
#
# 输出：./out-<customer_id>/.env  以及一份 credentials.txt（管理员账号密码）
set -euo pipefail

CUSTOMER_ID="${1:?用法: $0 <customer_id> <image_tag> <activation_code> [license_url] [bind_port]}"
IMAGE_TAG="${2:?缺少镜像 tag}"
ACTIVATION_CODE="${3:?缺少激活码}"
LICENSE_URL="${4:-https://license.lumos7.cc}"
BIND_PORT="${5:-80}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$REPO_ROOT/deploy/.env.example"
OUT_DIR="$REPO_ROOT/out-$CUSTOMER_ID"

[ -f "$TEMPLATE" ] || { echo "找不到模板 $TEMPLATE" >&2; exit 1; }
mkdir -p "$OUT_DIR"

# TOTP 密钥必须是十六进制，用字母数字混合会让容器无限重启：
#   invalid totp encryption key: encoding/hex: invalid byte
TOTP_KEY="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
# 其余密钥任意字符集即可。
rand() { python3 -c "import secrets,string; print(''.join(secrets.choice(string.ascii_letters+string.digits) for _ in range($1)))"; }
PG_PASS="$(rand 32)"
JWT="$(rand 48)"
ADMIN_PASS="$(rand 20)"
ADMIN_MAIL="admin@${CUSTOMER_ID}.local"

python3 - "$TEMPLATE" "$OUT_DIR/.env" <<PY
import re, sys
src, dst = sys.argv[1], sys.argv[2]
values = {
    "POSTGRES_PASSWORD": "$PG_PASS",
    "JWT_SECRET": "$JWT",
    "TOTP_ENCRYPTION_KEY": "$TOTP_KEY",
    "ADMIN_PASSWORD": "$ADMIN_PASS",
    "ADMIN_EMAIL": "$ADMIN_MAIL",
    "BIND_HOST": "0.0.0.0",
    "SERVER_PORT": "$BIND_PORT",
    "SERVER_MODE": "release",
}
out = []
for line in open(src).read().splitlines():
    m = re.match(r'^([A-Z_]+)=', line)
    out.append(f"{m.group(1)}={values[m.group(1)]}" if m and m.group(1) in values else line)
out += [
    "",
    "# ===== 客户部署授权（$CUSTOMER_ID）=====",
    "SUB2API_CUSTOMER_IMAGE=$IMAGE_TAG",
    "DEPLOYMENT_LICENSE_SERVER_URL=$LICENSE_URL",
    "DEPLOYMENT_LICENSE_ACTIVATION_CODE=$ACTIVATION_CODE",
    "DEPLOYMENT_LICENSE_RENEWAL_INTERVAL_SECONDS=3600",
    "DEPLOYMENT_LICENSE_REQUEST_TIMEOUT_SECONDS=10",
    "DEPLOYMENT_LICENSE_GRACE_PERIOD_SECONDS=21600",
    "DEPLOYMENT_LICENSE_PUBLIC_KEY=",
    "DEPLOYMENT_LICENSE_IMAGE_DIGEST=",
]
open(dst, "w").write("\n".join(out) + "\n")
PY

chmod 600 "$OUT_DIR/.env"

# 自检：TOTP 必须是 64 位纯 hex，否则容器起不来。
grep -qE '^TOTP_ENCRYPTION_KEY=[0-9a-f]{64}$' "$OUT_DIR/.env" \
  || { echo "TOTP_ENCRYPTION_KEY 格式错误，中止" >&2; exit 1; }

cp "$REPO_ROOT/deploy/docker-compose.local.yml" \
   "$REPO_ROOT/deploy/docker-compose.customer.yml" "$OUT_DIR/"

cat > "$OUT_DIR/credentials.txt" <<EOF
客户            $CUSTOMER_ID
镜像            $IMAGE_TAG
授权中心        $LICENSE_URL
后台地址        http://<客户服务器 IP>:$BIND_PORT
管理员邮箱      $ADMIN_MAIL
管理员密码      $ADMIN_PASS
EOF
chmod 600 "$OUT_DIR/credentials.txt"

echo "已生成 $OUT_DIR/"
echo "  .env / docker-compose.local.yml / docker-compose.customer.yml / credentials.txt"
echo
cat "$OUT_DIR/credentials.txt"
echo
echo "注意：out-* 目录含明文密钥，交付后请删除本地副本。"

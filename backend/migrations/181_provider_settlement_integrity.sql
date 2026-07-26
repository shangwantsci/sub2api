-- 供号商结算的财务完整性加固。
--
-- 解决三个问题：
--
-- 1) 迟到写入漏账。usage_logs 由异步 worker 写入，created_at 是 worker 赋值而非
--    COMMIT 时刻，因此可能出现「created_at 早、提交晚」。仅按 created_at 半开区间
--    封账会让这类记录既不在已封金额中、也进不了下一期。
--    这里给结算单加 last_usage_id 水位：下一期用
--    (created_at 落在本期区间) OR (created_at 早于本期起点 AND id > 上期水位)
--    捕获漏网行。id > 水位保证不会重复计入。
--
-- 2) 历史明细不可重算。结算单原本只存总额，明细靠回查 usage_logs 活表；而 usage_logs
--    默认 90 天后被硬删（dashboard 保留策略），届时历史结算单导不出可用凭证。
--    这里新建 provider_settlement_items 在封账时快照分账号明细。
--
-- 3) 并发重复结算。加 partial unique index，与应用层的 advisory lock 互为兜底。

-- ===== provider_settlements 加固 =====

ALTER TABLE provider_settlements
    ADD COLUMN IF NOT EXISTS last_usage_id BIGINT NOT NULL DEFAULT 0,
    -- 作废原因单独存：写进 notes 会覆盖结算时填的备注，破坏原始审计信息。
    ADD COLUMN IF NOT EXISTS void_reason TEXT;

COMMENT ON COLUMN provider_settlements.last_usage_id IS
    'Highest usage_logs.id counted in this period; next period uses it to catch late-committed rows';
COMMENT ON COLUMN provider_settlements.void_reason IS
    'Reason recorded when voiding; kept separate so the original settlement notes stay intact';

-- 同一供号商同一 period_end 只能有一条有效结算单。
-- 作废后 status 变为 voided 而不在此索引内，因此允许重新结算同一周期。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_provider_settlements_active_period
    ON provider_settlements (provider_user_id, period_end)
    WHERE status = 'settled';

ALTER TABLE provider_settlements
    DROP CONSTRAINT IF EXISTS provider_settlements_period_check,
    ADD CONSTRAINT provider_settlements_period_check
        CHECK (period_end > period_start),
    DROP CONSTRAINT IF EXISTS provider_settlements_amounts_check,
    ADD CONSTRAINT provider_settlements_amounts_check
        CHECK (standard_cost >= 0 AND requests >= 0 AND tokens >= 0
               AND account_count >= 0 AND last_usage_id >= 0),
    -- 已作废的单必须带作废时间，避免出现语义残缺的中间态。
    DROP CONSTRAINT IF EXISTS provider_settlements_voided_check,
    ADD CONSTRAINT provider_settlements_voided_check
        CHECK (status <> 'voided' OR voided_at IS NOT NULL);

-- ===== 结算明细快照 =====

CREATE TABLE IF NOT EXISTS provider_settlement_items (
    id            BIGSERIAL PRIMARY KEY,
    settlement_id BIGINT         NOT NULL
                  REFERENCES provider_settlements(id) ON DELETE CASCADE,
    account_id    BIGINT         NOT NULL,
    -- 账号名在封账时刻的快照。账号可能之后被改名或下线，凭证必须固化当时的名字。
    account_name  VARCHAR(100)   NOT NULL DEFAULT '',
    offline       BOOLEAN        NOT NULL DEFAULT FALSE,
    requests      BIGINT         NOT NULL DEFAULT 0,
    tokens        BIGINT         NOT NULL DEFAULT 0,
    standard_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_provider_settlement_items_account
    ON provider_settlement_items (settlement_id, account_id);

ALTER TABLE provider_settlement_items
    DROP CONSTRAINT IF EXISTS provider_settlement_items_amounts_check,
    ADD CONSTRAINT provider_settlement_items_amounts_check
        CHECK (standard_cost >= 0 AND requests >= 0 AND tokens >= 0);

COMMENT ON TABLE provider_settlement_items IS
    'Immutable per-account snapshot taken at settle time; exports read this instead of usage_logs';

-- provider_tier 热查询索引（「应用到存量」按档位回填时用）。
CREATE INDEX IF NOT EXISTS idx_accounts_provider_tier
    ON accounts (provider_tier) WHERE provider_user_id IS NOT NULL;

-- 供号商用户列表索引。供号商数量远小于普通用户，partial index 足够。
CREATE INDEX IF NOT EXISTS idx_users_is_provider
    ON users (id) WHERE is_provider = TRUE;

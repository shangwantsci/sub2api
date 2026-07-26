-- 供号商站点（Provider Portal）
--
-- 1) users.is_provider：供号商能力位。role 仍为 user，但只能访问 /provider 站点。
-- 2) accounts.provider_user_id：账号归属的供号商，结算按此聚合。
--    partial index 只覆盖非 NULL 行，因为绝大多数账号是管理员自己上的号。
-- 3) accounts.provider_tier：上号时选择的速率档位，用于「应用到存量」回填筛选。
-- 4) provider_settlements：结算封账单。结算不删改 usage_logs，只插入不可变记录，
--    当前待结算周期的起点取最近一条 status='settled' 的 period_end。

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_provider BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS provider_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS provider_tier VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_accounts_provider_user_id
    ON accounts (provider_user_id) WHERE provider_user_id IS NOT NULL;

COMMENT ON COLUMN users.is_provider IS
    'Provider portal capability flag; role stays "user" but consumer-side APIs are denied';
COMMENT ON COLUMN accounts.provider_user_id IS
    'Owning provider user id (NULL = onboarded by admin); settlement aggregates on this';
COMMENT ON COLUMN accounts.provider_tier IS
    'Capacity tier chosen at provider onboarding ("1".."5" or "custom")';

CREATE TABLE IF NOT EXISTS provider_settlements (
    id               BIGSERIAL PRIMARY KEY,
    provider_user_id BIGINT         NOT NULL,
    period_start     TIMESTAMPTZ    NOT NULL,
    period_end       TIMESTAMPTZ    NOT NULL,
    standard_cost    DECIMAL(20,10) NOT NULL DEFAULT 0,
    requests         BIGINT         NOT NULL DEFAULT 0,
    tokens           BIGINT         NOT NULL DEFAULT 0,
    account_count    INTEGER        NOT NULL DEFAULT 0,
    status           VARCHAR(20)    NOT NULL DEFAULT 'settled',
    settled_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    settled_by       BIGINT,
    voided_at        TIMESTAMPTZ,
    voided_by        BIGINT,
    notes            TEXT,
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_provider_settlements_provider
    ON provider_settlements (provider_user_id, period_end DESC);
CREATE INDEX IF NOT EXISTS idx_provider_settlements_status
    ON provider_settlements (status);

ALTER TABLE provider_settlements
    DROP CONSTRAINT IF EXISTS provider_settlements_status_check,
    ADD CONSTRAINT provider_settlements_status_check
        CHECK (status IN ('settled', 'voided'));

COMMENT ON TABLE provider_settlements IS
    'Immutable provider settlement records; settling never mutates usage_logs';
COMMENT ON COLUMN provider_settlements.standard_cost IS
    'Snapshot of SUM(usage_logs.total_cost) at 1x rate, full precision';

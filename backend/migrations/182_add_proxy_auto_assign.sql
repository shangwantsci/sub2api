-- 代理归属与自动分配池
--
-- 1) proxies.provider_user_id：代理归属。NULL = 平台自有，非空 = 该供号商上号时自带。
--    在此之前归属只靠记录名的 provider-<id>- 前缀约定，全仓库没有一行代码依赖它。
--    供号商上号新增「由平台分配 IP」后，选号按绑定账号数最少来挑，若不区分归属会
--    把供号商 A 自费的代理分给 B，两家账号还会共用出口 IP —— 后者直接违背本 fork
--    「每个账号看起来像独立真人」的核心目标。归属必须是可查询的列，不能是命名约定。
--
-- 2) proxies.auto_assignable：管理员是否把这条代理放进自动分配池。
--    默认 FALSE 是安全默认：升级后存量代理一条都不会被分配出去，必须管理员在代理
--    管理页逐条勾选。历史上供号商建的代理（provider_user_id 回填不到、只有名字前缀）
--    因此同样不会被误分配。

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS provider_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS auto_assignable  BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_proxies_provider_user_id
    ON proxies (provider_user_id) WHERE provider_user_id IS NOT NULL;

-- 自动分配的候选查询固定带 auto_assignable AND provider_user_id IS NULL AND 未软删，
-- partial index 只覆盖真正会被扫到的那一小撮行。
CREATE INDEX IF NOT EXISTS idx_proxies_auto_assign_pool
    ON proxies (status)
    WHERE auto_assignable AND provider_user_id IS NULL AND deleted_at IS NULL;

COMMENT ON COLUMN proxies.provider_user_id IS
    'Owning provider user id (NULL = platform-owned); provider-owned proxies never enter the auto-assign pool';
COMMENT ON COLUMN proxies.auto_assignable IS
    'Admin opt-in flag for the provider auto-assign pool; defaults FALSE so existing proxies are never handed out silently';

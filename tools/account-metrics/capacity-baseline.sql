-- 账号存活基线查询（只读）。
--
-- 用途：在启用或调整容量档位（capacity_presets）与动态降速（extra.adaptive_throttle）前后，
-- 对比账号的存活情况：改动前跑一遍留作基线，分阶段放开后再跑一遍对比。
-- 六组查询分别看：
--   [A] 账号寿命与死亡率（anthropic，含已软删除）
--   [B] 寿命与「每天活跃小时数」的关系（全天候在线是否缩短寿命）
--   [C] 每个账号的并发峰值与 p95（7 天），可对照 accounts.concurrency
--   [D] 复用广度：每个账号服务了多少个用户、活跃了多少小时（7 天）
--   [E] 全局并发峰值（容量规划用）
--   [F] 按小时统计的请求量分布
--
-- 这些查询只读 accounts 与 usage_logs 的常规列，不依赖任何 extra 键，
-- 因此不受容量模型改版影响。
--
--   docker exec -i sub2api-postgres psql -U sub2api -d sub2api -v ON_ERROR_STOP=0 -q -f - < tools/account-metrics/capacity-baseline.sql
--
-- （容器名与库名以实际部署为准。）[F] 里的时区写死为 Asia/Shanghai，请改成你要验证的用户活跃时区。
\pset pager off

\echo '==== [A] account mortality (anthropic, incl soft-deleted) ===='
SELECT status,
       count(*) AS n,
       count(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted,
       round(percentile_cont(0.5) WITHIN GROUP (
         ORDER BY EXTRACT(epoch FROM (coalesce(deleted_at, now()) - created_at))/86400)::numeric, 2) AS median_life_days
FROM accounts WHERE platform = 'anthropic'
GROUP BY status ORDER BY n DESC;

\echo '==== [B] lifespan vs daily-active-hours (does 24x7 shorten life?) ===='
WITH act AS (
  SELECT ul.account_id,
         count(*) AS reqs,
         count(distinct date_trunc('hour', ul.created_at)) AS lit_hours,
         count(distinct date(ul.created_at)) AS active_days
  FROM usage_logs ul
  WHERE ul.created_at >= now() - interval '30 days'
    AND ul.account_id IN (SELECT id FROM accounts WHERE platform = 'anthropic')
  GROUP BY ul.account_id HAVING count(*) >= 50
),
joined AS (
  SELECT EXTRACT(epoch FROM (coalesce(a.deleted_at, now()) - a.created_at))/86400 AS life_days,
         act.reqs,
         act.lit_hours::numeric / nullif(act.active_days, 0) AS hpd
  FROM act JOIN accounts a ON a.id = act.account_id
)
SELECT CASE WHEN hpd < 8 THEN '1) <8h/day' WHEN hpd < 14 THEN '2) 8-14h' WHEN hpd < 20 THEN '3) 14-20h' ELSE '4) >=20h' END AS bucket,
       count(*) AS accounts,
       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY life_days)::numeric, 2) AS median_life_days,
       round(avg(reqs)::numeric, 0) AS avg_reqs_30d
FROM joined GROUP BY bucket ORDER BY bucket;

\echo '==== [C] peak/p95 concurrency per anthropic account (7d, top 20) ===='
WITH ul AS (
  SELECT account_id, created_at AS s, created_at + (duration_ms || ' milliseconds')::interval AS e
  FROM usage_logs
  WHERE created_at >= now() - interval '7 days' AND duration_ms IS NOT NULL
    AND account_id IN (SELECT id FROM accounts WHERE platform = 'anthropic')
),
ev AS (SELECT account_id, s AS ts, 1 AS d FROM ul UNION ALL SELECT account_id, e AS ts, -1 AS d FROM ul),
run AS (SELECT account_id, SUM(d) OVER (PARTITION BY account_id ORDER BY ts, d DESC ROWS UNBOUNDED PRECEDING) AS c FROM ev)
SELECT r.account_id, a.name, a.concurrency AS cfg_conc,
       max(c) AS peak, round(percentile_cont(0.95) WITHIN GROUP (ORDER BY c)::numeric, 2) AS p95
FROM run r JOIN accounts a ON a.id = r.account_id
GROUP BY r.account_id, a.name, a.concurrency ORDER BY peak DESC LIMIT 20;

\echo '==== [D] multiplex breadth + active hours per account (7d, top 20) ===='
SELECT ul.account_id, a.name,
       count(*) AS reqs_7d,
       count(distinct ul.user_id) AS users,
       count(distinct date_trunc('hour', ul.created_at)) AS active_hours_7d
FROM usage_logs ul JOIN accounts a ON a.id = ul.account_id
WHERE ul.created_at >= now() - interval '7 days' AND a.platform = 'anthropic'
GROUP BY ul.account_id, a.name ORDER BY reqs_7d DESC LIMIT 20;

\echo '==== [E] system-wide peak concurrency (fleet sizing) ===='
WITH ul AS (
  SELECT created_at AS s, created_at + (duration_ms || ' milliseconds')::interval AS e
  FROM usage_logs
  WHERE created_at >= now() - interval '7 days' AND duration_ms IS NOT NULL
    AND account_id IN (SELECT id FROM accounts WHERE platform = 'anthropic')
),
ev AS (SELECT s AS ts, 1 AS d FROM ul UNION ALL SELECT e AS ts, -1 AS d FROM ul),
run AS (SELECT SUM(d) OVER (ORDER BY ts, d DESC ROWS UNBOUNDED PRECEDING) AS c FROM ev)
SELECT max(c) AS peak_concurrent,
       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY c)::numeric, 2) AS p50,
       round(percentile_cont(0.95) WITHIN GROUP (ORDER BY c)::numeric, 2) AS p95
FROM run;

\echo '==== [F] hour-of-day activity (adjust the timezone below to the demand timezone) ===='
SELECT EXTRACT(hour FROM created_at AT TIME ZONE 'Asia/Shanghai')::int AS hod, count(*) AS reqs
FROM usage_logs
WHERE created_at >= now() - interval '7 days'
  AND account_id IN (SELECT id FROM accounts WHERE platform = 'anthropic')
GROUP BY hod ORDER BY hod;

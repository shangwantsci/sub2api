-- Migration: 227_capacity_adaptive_throttle
--
-- 为什么做：
--   统一容量模型。旧的「防封档位」persona_*（运行时按名字解析 5/3/2 并发、50/30/15 RPM，
--   数值从未写进账号字段）与供货商 1-5 档规格表是两套数值来源。新模型里：
--     * 容量数值只存在账号字段：accounts.concurrency 与 extra.base_rpm 等官方键；
--     * 动态降速由账号上独立的开关 extra.adaptive_throttle（严格 JSON bool）控制；
--     * 养号由账号 extra.nurturing_enabled（严格 JSON bool）+ 全局开关控制；
--     * 档位只是「容量档位模板」（设置键 capacity_presets），选档位 = 把数值复制写入账号。
--   本迁移把旧数据翻译成新字段，并把账号上的 persona_* 键清掉。
--
-- 一、accounts（persona_* → concurrency / extra）
--   1. 只有 platform = 'anthropic' 的账号做翻译。任何平台（含已软删）的账号，只要 extra 是 JSON 对象
--      且带有下列 9 个键，就把它们清掉；除这 9 个键外 extra 里的其它键原样保留：
--        persona_enabled / persona_preset / persona_timezone / persona_locale /
--        persona_active_start_hour / persona_active_end_hour / persona_max_concurrency /
--        persona_daily_request_cap / persona_off_peak_concurrency
--      非 anthropic 账号只清键，不写新键，也不改 concurrency / load_factor。
--   2. 「启用」判定与旧代码 IsPersonaEnabled 一致：persona_preset 是 JSON 字符串且为
--      aggressive / balanced / conservative 之一则启用（优先）；persona_preset = 'off' 显式关闭；
--      其余情况看 persona_enabled 是否为严格 JSON true（字符串 "true"、数字 1 都不算）。
--      persona_preset 不是 JSON 字符串时按「未设置预设」处理（旧代码 GetExtraString 返回空串）。
--   3. 命中的 anthropic 账号：
--        extra.adaptive_throttle = true；
--        concurrency = persona_max_concurrency（解析后 > 0 才覆盖），否则取预设基数
--          aggressive 5 / balanced 3 / conservative 2；仅有 persona_enabled:true、
--          既无预设也无 max 的旧账号保持原 concurrency 列值；
--        extra.base_rpm 仅在「缺失或解析后 <= 0」时写预设 RPM（50 / 30 / 15），
--          已有 > 0 的 base_rpm 不动；无预设的旧账号没有预设值可写，base_rpm 不动；
--        extra.nurturing_enabled = true 只写给 type 为 oauth / setup-token 的账号
--          （旧养号服务只处理这两种类型）；
--        load_factor 非空且新并发 > 0 时与新并发对齐（旧选号路径对 persona 账号只看并发、
--          无视 load_factor；新语义下负载分母取 load_factor，对齐后分母保持不变）。
--      翻译的是账号级旧预设的原数值，不是新模板数值，以保持原有账号行为。
--      注意（单一开关的必然结果）：旧代码对「仅有 persona_enabled:true、既无预设也无 persona_max_concurrency」
--      的账号只按利用率降 RPM、不降并发；新模型里 adaptive_throttle 同时作用于并发与 RPM，
--      所以这类账号迁移后并发也会随利用率降速，比旧行为更保守。
--   4. persona_max_concurrency / base_rpm 的解析与旧代码 parseExtraInt 对齐：JSON 数字取整数部分
--      （4.7 → 4）；JSON 字符串按「去首尾空白后的十进制整数」解析（"4"、" 5 "、"+6" 有效，
--      "4.7"、"abc" 无效）；其余类型、负数、0 一律无效。persona_max_concurrency 超过 9 位的数值
--      按无效处理，避免溢出 integer 列。任何脏值都不会让迁移报错。
--   5. 丢弃（新模型不再支持）：日请求上限、作息窗口、非高峰并发、persona 时区与 locale。
--      旧「作息窗口外并发降到非高峰值」随之失效，并发恒取上面算出的值。
--   6. 未命中（preset = off、persona_enabled 非严格 true、非 anthropic）的账号只清键，不写新键。
--   7. 本语句绕过了应用层，所以对每个被 UPDATE 的账号写一条 scheduler_outbox（account_changed），
--      让调度快照刷新；写法同 175_default_openai_long_context_billing.sql。dedup_key 留空
--      （部分唯一索引只约束非空值），不会与应用写入的 pending 行冲突。
--
-- 二、settings（供货商速率档位表 → 容量档位模板）
--   provider_capacity_tiers（JSON 数组）→ capacity_presets（JSON 对象
--     {"presets":[{key,label,concurrency,max_sessions,base_rpm,window_cost_limit,adaptive_throttle}],
--      "default_key":"3"}）+ provider_enabled_tier_keys（JSON 字符串数组，取旧表里 enabled 严格为
--   true 的档位）。
--   * adaptive_throttle = 旧档位的 persona_preset 为 aggressive / balanced / conservative；
--   * 数值字段不是 JSON 数字时取缺省（concurrency 1，其余 0），concurrency 至少为 1，
--     window_cost_limit 允许小数且不小于 0；label 缺失或为空时用 tier；
--   * default_key：迁移出的 key 里有 '3' 用 '3'，否则取第一个 key；
--   * 旧表缺失、空串、非 JSON、非数组、没有任何带字符串 tier 的元素时一律跳过不写，
--     由应用端使用种子模板；元素缺 tier、tier 为空或为 custom、tier 重复（取第一个）的元素被跳过；
--   * 已存在 capacity_presets 时不覆盖；
--   * 旧的 provider_capacity_tiers / provider_default_tier 设置行原样保留（回退到旧代码仍读得到）。
--
-- 三、幂等性
--   第二次执行时账号上已经没有 persona_* 键，第一条语句命中 0 行，不会再写 outbox；
--   第二段发现 capacity_presets 已存在即返回，其余写入都带 ON CONFLICT (key) DO NOTHING。
--   迁移 runner 另有 schema_migrations 校验和，保证每个库只执行一次。

WITH persona_keys AS (
    SELECT ARRAY[
        'persona_enabled',
        'persona_preset',
        'persona_timezone',
        'persona_locale',
        'persona_active_start_hour',
        'persona_active_end_hour',
        'persona_max_concurrency',
        'persona_daily_request_cap',
        'persona_off_peak_concurrency'
    ]::text[] AS k
),
-- 带旧键的账号（extra 必须是 JSON 对象），把旧值解析成强类型
legacy AS (
    SELECT
        a.id,
        a.platform,
        a.type,
        a.concurrency,
        -- 与旧代码 GetExtraString 一致：非字符串视为空
        CASE WHEN jsonb_typeof(a.extra->'persona_preset') = 'string'
             THEN a.extra->>'persona_preset' END AS preset,
        (a.extra->'persona_enabled' IS NOT DISTINCT FROM 'true'::jsonb) AS enabled_flag,
        -- 与旧代码 parseExtraInt 一致：JSON 数字取整数部分，JSON 字符串按 Atoi(TrimSpace)；
        -- 先用正则守卫再 cast，最多 9 位，不会溢出
        CASE jsonb_typeof(a.extra->'persona_max_concurrency')
            WHEN 'number' THEN substring(a.extra->>'persona_max_concurrency'
                                         from '^([0-9]{1,9})(?:[.][0-9]+)?$')::int
            WHEN 'string' THEN substring(a.extra->>'persona_max_concurrency'
                                         from '^[[:space:]]*[+]?([0-9]{1,9})[[:space:]]*$')::int
        END AS max_conc,
        -- 与旧代码 GetBaseRPM 一致：解析后 > 0 才算「已配置」
        COALESCE(CASE jsonb_typeof(a.extra->'base_rpm')
            WHEN 'number' THEN (a.extra->>'base_rpm') ~ '^[0-9]*[1-9][0-9]*(?:[.][0-9]+)?$'
            WHEN 'string' THEN (a.extra->>'base_rpm') ~ '^[[:space:]]*[+]?[0-9]*[1-9][0-9]*[[:space:]]*$'
        END, false) AS has_base_rpm
    FROM accounts AS a, persona_keys AS pk
    WHERE jsonb_typeof(a.extra) = 'object'
      AND a.extra ?| pk.k
),
-- on_ = 旧代码认定的「启用」且是 anthropic 账号；预设基数（并发、RPM）来自 VALUES
hit AS (
    SELECT
        l.id,
        l.type,
        l.concurrency,
        l.max_conc,
        l.has_base_rpm,
        (l.platform = 'anthropic'
         AND (p.name IS NOT NULL
              OR (l.preset IS DISTINCT FROM 'off' AND l.enabled_flag))) AS on_,
        p.conc AS preset_conc,
        p.rpm AS preset_rpm
    FROM legacy AS l
    LEFT JOIN (VALUES ('aggressive', 5, 50), ('balanced', 3, 30), ('conservative', 2, 15))
        AS p(name, conc, rpm) ON p.name = l.preset
),
calc AS (
    SELECT
        h.*,
        CASE WHEN h.on_ THEN COALESCE(NULLIF(h.max_conc, 0), h.preset_conc, h.concurrency)
             ELSE h.concurrency END AS new_conc
    FROM hit AS h
),
upd AS (
    UPDATE accounts AS a
    SET concurrency = c.new_conc,
        load_factor = CASE WHEN c.on_ AND a.load_factor IS NOT NULL AND c.new_conc > 0
                           THEN c.new_conc ELSE a.load_factor END,
        extra = (a.extra - pk.k)
                || CASE WHEN c.on_
                        THEN jsonb_build_object('adaptive_throttle', true)
                        ELSE '{}'::jsonb END
                || CASE WHEN c.on_ AND c.type IN ('oauth', 'setup-token')
                        THEN jsonb_build_object('nurturing_enabled', true)
                        ELSE '{}'::jsonb END
                || CASE WHEN c.on_ AND NOT c.has_base_rpm AND c.preset_rpm IS NOT NULL
                        THEN jsonb_build_object('base_rpm', c.preset_rpm)
                        ELSE '{}'::jsonb END,
        updated_at = NOW()
    FROM calc AS c, persona_keys AS pk
    WHERE a.id = c.id
    RETURNING a.id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', u.id
FROM upd AS u;

DO $$
DECLARE
    v_tiers        jsonb;
    v_presets      jsonb;
    v_enabled_keys jsonb;
BEGIN
    IF EXISTS (SELECT 1 FROM settings WHERE key = 'capacity_presets') THEN
        RETURN;
    END IF;

    SELECT s.value::jsonb INTO v_tiers
    FROM settings AS s
    WHERE s.key = 'provider_capacity_tiers' AND btrim(s.value) <> '';

    IF v_tiers IS NULL OR jsonb_typeof(v_tiers) <> 'array' THEN
        RETURN;
    END IF;

    WITH parsed AS (
        SELECT
            btrim(e.t->>'tier') AS tier_key,
            e.o AS ord,
            COALESCE(NULLIF(btrim(CASE WHEN jsonb_typeof(e.t->'label') = 'string'
                                       THEN e.t->>'label' END), ''),
                     btrim(e.t->>'tier')) AS label,
            GREATEST(COALESCE(CASE jsonb_typeof(e.t->'concurrency')
                WHEN 'number' THEN substring(e.t->>'concurrency'
                                             from '^([0-9]{1,9})(?:[.][0-9]+)?$')::int
            END, 1), 1) AS concurrency,
            COALESCE(CASE jsonb_typeof(e.t->'max_sessions')
                WHEN 'number' THEN substring(e.t->>'max_sessions'
                                             from '^([0-9]{1,9})(?:[.][0-9]+)?$')::int
            END, 0) AS max_sessions,
            COALESCE(CASE jsonb_typeof(e.t->'base_rpm')
                WHEN 'number' THEN substring(e.t->>'base_rpm'
                                             from '^([0-9]{1,9})(?:[.][0-9]+)?$')::int
            END, 0) AS base_rpm,
            CASE WHEN jsonb_typeof(e.t->'window_cost_limit') = 'number'
                 THEN GREATEST((e.t->>'window_cost_limit')::numeric, 0)
                 ELSE 0 END AS window_cost_limit,
            COALESCE((CASE WHEN jsonb_typeof(e.t->'persona_preset') = 'string'
                           THEN e.t->>'persona_preset' END)
                     IN ('aggressive', 'balanced', 'conservative'), false) AS adaptive_throttle,
            (e.t->'enabled' IS NOT DISTINCT FROM 'true'::jsonb) AS enabled
        FROM jsonb_array_elements(v_tiers) WITH ORDINALITY AS e(t, o)
        WHERE jsonb_typeof(e.t) = 'object'
          AND jsonb_typeof(e.t->'tier') = 'string'
    ),
    valid AS (
        SELECT DISTINCT ON (tier_key) *
        FROM parsed
        WHERE tier_key <> '' AND tier_key <> 'custom'
        ORDER BY tier_key, ord
    )
    SELECT
        jsonb_build_object(
            'presets', jsonb_agg(
                jsonb_build_object(
                    'key', v.tier_key,
                    'label', v.label,
                    'concurrency', v.concurrency,
                    'max_sessions', v.max_sessions,
                    'base_rpm', v.base_rpm,
                    'window_cost_limit', v.window_cost_limit,
                    'adaptive_throttle', v.adaptive_throttle
                ) ORDER BY v.ord),
            'default_key', CASE WHEN bool_or(v.tier_key = '3') THEN '3'
                                ELSE (array_agg(v.tier_key ORDER BY v.ord))[1] END
        ),
        COALESCE(jsonb_agg(v.tier_key ORDER BY v.ord) FILTER (WHERE v.enabled), '[]'::jsonb)
    INTO v_presets, v_enabled_keys
    FROM valid AS v
    HAVING count(*) > 0;

    IF v_presets IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO settings (key, value)
    VALUES ('capacity_presets', v_presets::text)
    ON CONFLICT (key) DO NOTHING;

    INSERT INTO settings (key, value)
    VALUES ('provider_enabled_tier_keys', v_enabled_keys::text)
    ON CONFLICT (key) DO NOTHING;
EXCEPTION WHEN data_exception THEN
    -- 旧设置里出现无法预料的脏数据（SQLSTATE 22 类）时跳过翻译，应用端使用种子模板，
    -- 不能让迁移失败导致服务起不来；非数据类错误（锁、磁盘等）仍然正常抛出，不会被悄悄吞掉。
    RAISE WARNING '[migration-227] skip capacity_presets translation: %', SQLERRM;
END $$;

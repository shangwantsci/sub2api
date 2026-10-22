package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const capacityAdaptiveThrottleMigration = "227_capacity_adaptive_throttle.sql"

// legacyPersonaExtraKeys 是旧「防封档位」写进 accounts.extra 的全部 9 个键，迁移必须把它们清掉。
var legacyPersonaExtraKeys = []string{
	"persona_enabled",
	"persona_preset",
	"persona_timezone",
	"persona_locale",
	"persona_active_start_hour",
	"persona_active_end_hour",
	"persona_max_concurrency",
	"persona_daily_request_cap",
	"persona_off_peak_concurrency",
}

// readMigration227 返回迁移全文，以及去掉整行注释后的可执行部分。
// 「不含某语句」这类断言只看可执行部分，避免被文件头的说明文字干扰。
func readMigration227(t *testing.T) (full string, code string) {
	t.Helper()

	content, err := FS.ReadFile(capacityAdaptiveThrottleMigration)
	require.NoError(t, err)

	full = string(content)
	lines := strings.Split(full, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return full, strings.Join(kept, "\n")
}

func TestMigration227ClearsExactlyTheNineLegacyPersonaKeys(t *testing.T) {
	_, code := readMigration227(t)

	for _, key := range legacyPersonaExtraKeys {
		require.Contains(t, code, "'"+key+"'", "legacy key %s must be listed", key)
	}
	// 只清这 9 个键：筛选与清除共用同一份键数组，用 jsonb - text[] 删除，不按前缀通配。
	require.Contains(t, code, "WITH persona_keys AS")
	require.Contains(t, code, "a.extra ?| pk.k")
	require.Contains(t, code, "(a.extra - pk.k)")
	require.NotContains(t, code, "LIKE")
	// extra 不是 JSON 对象的畸形行不处理，避免对非对象调用 jsonb_each 等函数而抛错。
	require.Contains(t, code, "jsonb_typeof(a.extra) = 'object'")
}

func TestMigration227TranslatesAnthropicPersonaToCapacityFields(t *testing.T) {
	_, code := readMigration227(t)

	// 仅 anthropic 翻译；非 anthropic 账号只清键。
	require.Contains(t, code, "l.platform = 'anthropic'")

	// 判定与旧 IsPersonaEnabled 一致：合法预设优先，off 显式关闭，其余看严格 JSON true。
	require.Contains(t, code, "jsonb_typeof(a.extra->'persona_preset') = 'string'")
	require.Contains(t, code, "l.preset IS DISTINCT FROM 'off'")
	require.Contains(t, code, "a.extra->'persona_enabled' IS NOT DISTINCT FROM 'true'::jsonb")

	// 预设基数：并发 5/3/2，RPM 50/30/15。
	require.Contains(t, code, "VALUES ('aggressive', 5, 50), ('balanced', 3, 30), ('conservative', 2, 15)")

	// 新键都是严格 JSON bool；养号只写给 oauth / setup-token。
	require.Contains(t, code, "jsonb_build_object('adaptive_throttle', true)")
	require.Contains(t, code, "jsonb_build_object('nurturing_enabled', true)")
	require.Contains(t, code, "c.type IN ('oauth', 'setup-token')")

	// 并发：persona_max_concurrency（>0）优先，其次预设基数，最后保持原列值。
	require.Contains(t, code, "COALESCE(NULLIF(h.max_conc, 0), h.preset_conc, h.concurrency)")
	// base_rpm 只在缺失或 <= 0 时写预设值。
	require.Contains(t, code, "NOT c.has_base_rpm AND c.preset_rpm IS NOT NULL")
	require.Contains(t, code, "jsonb_build_object('base_rpm', c.preset_rpm)")
	// load_factor 非空时与新并发对齐。
	require.Contains(t, code, "a.load_factor IS NOT NULL AND c.new_conc > 0")

	// 旧值解析先用正则守卫再 cast（最多 9 位，不会溢出 integer 列），JSON 数字与字符串分开处理。
	require.Contains(t, code, "CASE jsonb_typeof(a.extra->'persona_max_concurrency')")
	require.Contains(t, code, "WHEN 'number' THEN")
	require.Contains(t, code, "WHEN 'string' THEN")
	require.Contains(t, code, "[0-9]{1,9}")
}

func TestMigration227NotifiesSchedulerSnapshotForUpdatedAccounts(t *testing.T) {
	_, code := readMigration227(t)

	// 绕过应用层的 UPDATE 必须写 outbox，且只为真正被 UPDATE 的行写。
	require.Contains(t, code, "RETURNING a.id")
	require.Contains(t, code, "INSERT INTO scheduler_outbox (event_type, account_id)")
	require.Contains(t, code, "SELECT 'account_changed', u.id")
	require.Contains(t, code, "FROM upd AS u")
	require.Contains(t, code, "updated_at = NOW()")
}

func TestMigration227TranslatesProviderTiersToCapacityPresets(t *testing.T) {
	_, code := readMigration227(t)

	require.Contains(t, code, "'provider_capacity_tiers'")
	require.Contains(t, code, "'capacity_presets'")
	require.Contains(t, code, "'provider_enabled_tier_keys'")

	// 新模板是 JSON 对象，字段名与 Go 端 json tag 逐字一致。
	for _, field := range []string{
		"'presets'", "'default_key'", "'key'", "'label'", "'concurrency'",
		"'max_sessions'", "'base_rpm'", "'window_cost_limit'", "'adaptive_throttle'",
	} {
		require.Contains(t, code, field, "capacity_presets field %s", field)
	}

	// adaptive_throttle 取旧档位的 persona_preset 是否为合法预设；enabled 只认严格 JSON true。
	require.Contains(t, code, "IN ('aggressive', 'balanced', 'conservative')")
	require.Contains(t, code, "e.t->'enabled' IS NOT DISTINCT FROM 'true'::jsonb")

	// default_key：有 '3' 用 '3'，否则取第一个 key。
	require.Contains(t, code, "bool_or(v.tier_key = '3')")
	require.Contains(t, code, "(array_agg(v.tier_key ORDER BY v.ord))[1]")

	// 脏数据安全跳过：非数组、无有效元素、tier 非字符串 / 为空 / custom / 重复、数值非数字。
	require.Contains(t, code, "jsonb_typeof(v_tiers) <> 'array'")
	require.Contains(t, code, "jsonb_typeof(e.t) = 'object'")
	require.Contains(t, code, "jsonb_typeof(e.t->'tier') = 'string'")
	require.Contains(t, code, "tier_key <> '' AND tier_key <> 'custom'")
	require.Contains(t, code, "SELECT DISTINCT ON (tier_key) *")
	require.Contains(t, code, "HAVING count(*) > 0")
	require.Contains(t, code, "GREATEST(COALESCE(CASE jsonb_typeof(e.t->'concurrency')")
	require.Contains(t, code, "GREATEST((e.t->>'window_cost_limit')::numeric, 0)")
	require.Contains(t, code, "EXCEPTION WHEN data_exception THEN")
}

func TestMigration227IsIdempotentAndKeepsLegacySettings(t *testing.T) {
	_, code := readMigration227(t)

	// 账号部分：只有一条 UPDATE，筛选条件依赖旧键，第二次执行时旧键已清除因此命中 0 行。
	require.Equal(t, 1, strings.Count(code, "UPDATE accounts"))
	require.Contains(t, code, "AND a.extra ?| pk.k")

	// settings 部分：已有 capacity_presets 时直接返回；所有写入都不覆盖已有行。
	require.Contains(t, code, "IF EXISTS (SELECT 1 FROM settings WHERE key = 'capacity_presets') THEN")
	require.Equal(t, 2, strings.Count(code, "INSERT INTO settings (key, value)"))
	require.Equal(t, 2, strings.Count(code, "ON CONFLICT (key) DO NOTHING"))
	require.NotContains(t, code, "DO UPDATE")

	// 旧设置行（provider_capacity_tiers / provider_default_tier）只读不删不改，回退到旧代码仍读得到。
	require.NotContains(t, code, "UPDATE settings")
	require.NotContains(t, code, "provider_default_tier")

	// 纯数据迁移：没有任何 DDL，也没有删除语句；普通迁移不得含 CONCURRENTLY。
	upper := strings.ToUpper(code)
	for _, forbidden := range []string{
		"DELETE FROM", "DROP ", "ALTER ", "CREATE ", "TRUNCATE", "CONCURRENTLY",
	} {
		require.NotContains(t, upper, forbidden, "migration 227 must stay a data-only migration")
	}
}

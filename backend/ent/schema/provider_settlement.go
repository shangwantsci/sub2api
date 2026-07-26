package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/shopspring/decimal"
)

// ProviderSettlement 定义供号商结算单实体。
//
// 结算采用「封账」而非物理清零：每次结算插入一条不可变记录，usage_logs 一行不动。
// 当前待结算周期的起点 = 最近一条 status='settled' 的 period_end，没有则取供号商注册时间。
// 区间语义为半开区间 [period_start, period_end)，保证不重不漏。
//
// 作废最近一期会把 status 置为 'voided'，周期起点自动回退到上上期的 period_end，
// 该期金额随之回到待结算，因此误操作可恢复。
type ProviderSettlement struct {
	ent.Schema
}

func (ProviderSettlement) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "provider_settlements"},
	}
}

func (ProviderSettlement) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("provider_user_id"),

		field.Time("period_start").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("period_end").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		// standard_cost: 该期 1 倍率金额快照，等于 SUM(usage_logs.total_cost)。
		// GoType 用 shopspring/decimal 而非 float64：这是要拿去付钱的数字，
		// 从 SQL 扫描到 JSON 序列化必须全程十进制精确，不能中途降级为二进制浮点。
		// Default 用 DefaultFunc 而非 Default(0)：字段 GoType 已换成 decimal.Decimal，
		// float64 字面量无法隐式转换。
		field.Float("standard_cost").
			GoType(decimal.Decimal{}).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			DefaultFunc(func() decimal.Decimal { return decimal.Zero }),
		field.Int64("requests").Default(0),
		field.Int64("tokens").Default(0),
		field.Int("account_count").Default(0),

		// last_usage_id: 本期计入的最大 usage_logs.id，作为封账水位。
		// 下一期用它捕获「created_at 早于本期起点但提交更晚」的迟到行，
		// 同时保证这些行不会被重复计入。
		field.Int64("last_usage_id").Default(0),

		field.String("status").
			MaxLen(20).
			Default("settled"),

		field.Time("settled_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("settled_by").
			Optional().
			Nillable(),
		field.Time("voided_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("voided_by").
			Optional().
			Nillable(),
		// notes 是结算时填的备注（如「7 月结算，已转账」），一经写入不再修改。
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		// void_reason 与 notes 分开存：作废原因若覆盖原备注会破坏原始审计信息。
		field.String("void_reason").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),

		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ProviderSettlement) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_user_id", "period_end"),
		index.Fields("status"),
	}
}

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

// ProviderSettlementItem 是结算单的分账号明细快照。
//
// 之所以要落盘而不是每次回查 usage_logs：usage_logs 默认 90 天后被保留策略硬删，
// 管理员也可手工发起清理任务。若历史明细依赖活表重算，届时导出的对账凭证会残缺甚至为空，
// 而结算单上的总额却还在——供号商真要核对时拿不出东西。
//
// 快照一旦写入不再修改。账号名也一并固化：账号可能之后被改名或下线。
type ProviderSettlementItem struct {
	ent.Schema
}

func (ProviderSettlementItem) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "provider_settlement_items"},
	}
}

func (ProviderSettlementItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("settlement_id"),
		field.Int64("account_id"),
		field.String("account_name").
			MaxLen(100).
			Default(""),
		// offline 表示快照时该账号已被供号商下线；其金额仍计入本期。
		field.Bool("offline").Default(false),
		field.Int64("requests").Default(0),
		field.Int64("tokens").Default(0),
		field.Float("standard_cost").
			GoType(decimal.Decimal{}).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			DefaultFunc(func() decimal.Decimal { return decimal.Zero }),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ProviderSettlementItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("settlement_id", "account_id").Unique(),
		index.Fields("settlement_id"),
	}
}

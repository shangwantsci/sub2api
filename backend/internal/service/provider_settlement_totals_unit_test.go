//go:build unit

package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// 结算金额与明细快照同源，靠的就是这个函数。它一旦算错，结算单与导出凭证就会对不上：
// 管理员按明细付款是多付，按结算单付是少付，两边都没有报错。
func TestAggregateProviderTotals(t *testing.T) {
	t.Run("空明细归零", func(t *testing.T) {
		out := aggregateProviderTotals(nil)
		require.Equal(t, int64(0), out.Requests)
		require.Equal(t, int64(0), out.Tokens)
		require.True(t, out.StandardCost.IsZero())
		require.Equal(t, 0, out.AccountCount)
		require.Equal(t, int64(0), out.MaxUsageID)
		require.Equal(t, int64(0), out.LateRows)
	})

	t.Run("逐项汇总", func(t *testing.T) {
		out := aggregateProviderTotals([]ProviderAccountUsage{
			{AccountID: 1, Requests: 3, Tokens: 100, StandardCost: decimal.RequireFromString("0.05944"), MaxUsageID: 900, LateRows: 1},
			{AccountID: 2, Requests: 7, Tokens: 250, StandardCost: decimal.RequireFromString("1.2"), MaxUsageID: 1200},
			{AccountID: 3, Requests: 1, Tokens: 10, StandardCost: decimal.RequireFromString("0.00001"), MaxUsageID: 1100, LateRows: 2},
		})
		require.Equal(t, int64(11), out.Requests)
		require.Equal(t, int64(360), out.Tokens)
		require.Equal(t, 3, out.AccountCount)
		require.Equal(t, int64(3), out.LateRows)
		require.True(t, out.StandardCost.Equal(decimal.RequireFromString("1.25945")),
			"got %s", out.StandardCost)
	})

	// 水位取所有明细里的最大 id，不是最后一条的 id：ORDER BY 是按金额排的，
	// 取错会让水位比实际计入的行低，下期把已付过的行重新捕获一遍。
	t.Run("水位取最大而不是最后一条", func(t *testing.T) {
		out := aggregateProviderTotals([]ProviderAccountUsage{
			{AccountID: 1, Requests: 1, StandardCost: decimal.RequireFromString("9"), MaxUsageID: 500},
			{AccountID: 2, Requests: 1, StandardCost: decimal.RequireFromString("1"), MaxUsageID: 4200},
			{AccountID: 3, Requests: 1, StandardCost: decimal.RequireFromString("0.5"), MaxUsageID: 300},
		})
		require.Equal(t, int64(4200), out.MaxUsageID)
	})

	// 金额是要拿去付钱的，必须精确十进制累加，不能出现浮点尾巴。
	t.Run("金额精确十进制累加", func(t *testing.T) {
		items := make([]ProviderAccountUsage, 0, 10)
		for i := 0; i < 10; i++ {
			items = append(items, ProviderAccountUsage{
				AccountID:    int64(i + 1),
				Requests:     1,
				StandardCost: decimal.RequireFromString("0.1"),
			})
		}
		out := aggregateProviderTotals(items)
		require.True(t, out.StandardCost.Equal(decimal.RequireFromString("1")),
			"got %s", out.StandardCost)
	})

	t.Run("零用量账号不计入账号数", func(t *testing.T) {
		out := aggregateProviderTotals([]ProviderAccountUsage{
			{AccountID: 1, Requests: 0, StandardCost: decimal.Zero},
			{AccountID: 2, Requests: 5, StandardCost: decimal.RequireFromString("1")},
		})
		require.Equal(t, 1, out.AccountCount)
	})
}

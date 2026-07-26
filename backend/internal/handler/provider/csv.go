package provider

import (
	"github.com/shopspring/decimal"
)

// CSVCell 转义一个 CSV 单元格，防止电子表格公式注入。
//
// 供号商可以把账号名设成 =HYPERLINK(...)、+cmd、-1+1、@SUM(...) 之类的内容。
// encoding/csv 的引号只保证字段边界正确，Excel / Numbers / Sheets 打开时仍会把
// 这些首字符当公式执行。这里在危险首字符前补一个单引号，让表格按文本处理。
//
// 管理端与供号商端的导出必须共用这个函数，否则会留下一边有防护一边没有的缺口。
func CSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// CSVAmount 把金额渲染成 CSV 单元格。
//
// 输出完整精度的十进制字符串，不做 1 位小数收敛：CSV 是拿去核算的凭证，
// 展示层的舍入只应发生在界面上。
func CSVAmount(v decimal.Decimal) string {
	return v.String()
}

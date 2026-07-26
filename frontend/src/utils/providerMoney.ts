/**
 * 供号商对账金额处理。
 *
 * 后端把金额序列化成十进制字符串（如 `"1.2345678901"`）而不是 JSON 数字，
 * 因为 JSON 数字在 JS 侧会被解析成 float64，十进制精度当场丢失。
 * 这里全程按字符串处理，用 BigInt 做精确算术，只在最终展示时收敛到 1 位小数。
 *
 * 所有金额展示必须走这里，组件里不要各自 toFixed —— 对账场景下两个地方口径不一致
 * 会直接引发争议。CSV 导出不走格式化，直接输出后端给的完整精度原值。
 *
 * 绝不能先四舍五入再累加：单笔金额普遍在 0.0x 量级，逐笔进位会产生显著累积误差。
 */

/** 后端金额列为 decimal(20,10)，缩放因子与之对齐。 */
const SCALE = 10
const SCALE_FACTOR = 10n ** BigInt(SCALE)

/** 小于该值的正数显示为 `<$0.1`，避免被误读成完全没跑量。 */
const SMALL_AMOUNT_SCALED = SCALE_FACTOR / 20n // 0.05

/** 后端金额的传输类型。历史数据或异常情况下可能仍是 number。 */
export type MoneyInput = string | number | null | undefined

/**
 * 把金额解析成按 10^10 缩放的 BigInt，全程无精度损失。
 *
 * 无法解析时返回 0n 而不是抛错：对账页面上一个格式异常的字段
 * 不该让整个页面白屏，异常值会在一致性自检里被暴露出来。
 */
export function parseMoney(value: MoneyInput): bigint {
  if (value === null || value === undefined || value === '') return 0n

  // number 入参说明后端漏了字符串序列化，精度已经损失，这里只能尽力还原。
  const raw = typeof value === 'number' ? (Number.isFinite(value) ? value.toFixed(SCALE) : '0') : String(value).trim()

  const match = /^(-?)(\d*)(?:\.(\d*))?$/.exec(raw)
  if (!match) return 0n

  const [, sign, intPart = '', fracRaw = ''] = match
  if (intPart === '' && fracRaw === '') return 0n

  // 截断而非四舍五入多余位数：超出 decimal(20,10) 的位不该存在，
  // 若存在也不能靠进位放大金额。
  const frac = fracRaw.slice(0, SCALE).padEnd(SCALE, '0')
  const magnitude = BigInt(intPart || '0') * SCALE_FACTOR + BigInt(frac || '0')
  return sign === '-' ? -magnitude : magnitude
}

/** 精确求和，用于明细合计与一致性自检。 */
export function sumMoney(values: MoneyInput[]): bigint {
  return values.reduce<bigint>((acc, v) => acc + parseMoney(v), 0n)
}

/** 把缩放后的 BigInt 还原成完整精度的十进制字符串。 */
export function scaledToString(scaled: bigint): string {
  const negative = scaled < 0n
  const abs = negative ? -scaled : scaled
  const intPart = abs / SCALE_FACTOR
  const fracPart = (abs % SCALE_FACTOR).toString().padStart(SCALE, '0').replace(/0+$/, '')
  const body = fracPart ? `${intPart}.${fracPart}` : `${intPart}`
  return negative ? `-${body}` : body
}

/** 把缩放后的 BigInt 收敛成 1 位小数字符串，四舍五入（half-up）。 */
function scaledToFixedOne(scaled: bigint): string {
  const negative = scaled < 0n
  const abs = negative ? -scaled : scaled

  // 收敛到 1 位小数：先按 10^9 分组，再对余数做 half-up 进位。
  const unit = SCALE_FACTOR / 10n
  let tenths = abs / unit
  if (abs % unit >= unit / 2n) tenths += 1n

  const body = `${tenths / 10n}.${tenths % 10n}`
  return negative ? `-${body}` : body
}

/**
 * 格式化为 1 位小数的美元字符串。
 *
 * 大于 0 且小于 0.05 时返回 `<$0.1`：直接四舍五入会显示成 `$0.0`，
 * 供号商会以为该账号完全没有用量。
 */
export function formatUSD(value: MoneyInput): string {
  const scaled = parseMoney(value)
  if (scaled > 0n && scaled < SMALL_AMOUNT_SCALED) return '<$0.1'
  if (scaled < 0n && -scaled < SMALL_AMOUNT_SCALED) return '>-$0.1'
  return `$${scaledToFixedOne(scaled)}`
}

/** 不带货币符号的 1 位小数，用于表格右对齐数字列。 */
export function formatUSDValue(value: MoneyInput): string {
  return scaledToFixedOne(parseMoney(value))
}

/** 完整精度原值，用于导出与悬浮提示。 */
export function formatUSDExact(value: MoneyInput): string {
  return scaledToString(parseMoney(value))
}

/** 千分位整数，用于请求数与 token 数。 */
export function formatCount(value: number | string | null | undefined): string {
  const count = Number(value ?? 0)
  if (!Number.isFinite(count)) return '0'
  return count.toLocaleString('en-US')
}

/**
 * 转义 CSV 单元格，防止电子表格公式注入。
 *
 * 供号商可以把账号名设成 =HYPERLINK(...)、+cmd、@SUM(...) 之类的内容。
 * 引号只保证字段边界正确，Excel / Numbers / Sheets 打开时仍会把这些首字符当公式执行。
 * 在危险首字符前补一个单引号，让表格按纯文本处理。
 *
 * 后端导出有对应的 CSVCell，两边必须保持一致，否则会留下一边有防护一边没有的缺口。
 */
export function csvCell(value: string): string {
  if (!value) return value
  return /^[=+\-@\t\r]/.test(value) ? `'${value}` : value
}

/**
 * 判断两个金额是否完全相等。
 *
 * 用于结算前的一致性自检：明细逐行合计与后端给的总额若对不上，说明取数口径出了问题，
 * 此时必须拦住结算而不是让管理员照着两个不同的数字二选一。
 *
 * 这里要求精确相等而非容差内接近：两边都是同源的十进制值，用 BigInt 比较不存在
 * 浮点误差，任何差异都是真实的口径问题，不该被容差掩盖。
 */
export function amountsMatch(a: MoneyInput, b: MoneyInput): boolean {
  return parseMoney(a) === parseMoney(b)
}

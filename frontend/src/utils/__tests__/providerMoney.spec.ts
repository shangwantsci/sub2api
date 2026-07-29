import { describe, expect, it } from 'vitest'

import {
  amountsMatch,
  csvCell,
  formatCount,
  formatUSD,
  formatUSDExact,
  formatUSDValue,
  parseMoney,
  scaledToString,
  sumMoney,
} from '../providerMoney'

describe('parseMoney', () => {
  it('保住 decimal(20,10) 的全部有效位', () => {
    expect(scaledToString(parseMoney('1.2345678901'))).toBe('1.2345678901')
    expect(scaledToString(parseMoney('0.0000000001'))).toBe('0.0000000001')
  })

  it('处理大额而不丢精度', () => {
    // 这个值超过 Number.MAX_SAFE_INTEGER 缩放后的范围，float64 必然出错。
    expect(scaledToString(parseMoney('12345678901.2345678901'))).toBe('12345678901.2345678901')
  })

  it('接受整数、负数与缺省小数部分', () => {
    expect(scaledToString(parseMoney('42'))).toBe('42')
    expect(scaledToString(parseMoney('-1.5'))).toBe('-1.5')
    expect(scaledToString(parseMoney('.5'))).toBe('0.5')
  })

  it('异常输入回落为 0 而不是抛错', () => {
    // 对账页面上一个格式异常的字段不该让整页白屏。
    expect(parseMoney(null)).toBe(0n)
    expect(parseMoney(undefined)).toBe(0n)
    expect(parseMoney('')).toBe(0n)
    expect(parseMoney('abc')).toBe(0n)
    expect(parseMoney(Number.NaN)).toBe(0n)
  })

  it('截断超出 10 位的部分而不进位', () => {
    // 多余位不该存在；若存在也不能靠进位放大金额。
    expect(scaledToString(parseMoney('0.99999999999'))).toBe('0.9999999999')
  })
})

describe('sumMoney', () => {
  it('逐笔精确累加，不产生浮点误差', () => {
    // float64 下 0.1 + 0.2 !== 0.3，这里必须精确。
    expect(scaledToString(sumMoney(['0.1', '0.2']))).toBe('0.3')
  })

  it('大量小额累加与逐位相加结果一致', () => {
    const rows = Array.from({ length: 1000 }, () => '0.0000000001')
    expect(scaledToString(sumMoney(rows))).toBe('0.0000001')
  })

  it('明细合计与后端总额精确吻合时通过一致性自检', () => {
    const accounts = ['1.1111111111', '2.2222222222', '3.3333333333']
    const total = scaledToString(sumMoney(accounts))
    expect(total).toBe('6.6666666666')
    expect(amountsMatch(total, '6.6666666666')).toBe(true)
  })
})

describe('formatUSD', () => {
  it('常规金额用 2 位小数', () => {
    expect(formatUSD('12.34')).toBe('$12.34')
    expect(formatUSD('12.345')).toBe('$12.35')
    expect(formatUSD('29.3089292500')).toBe('$29.31')
    expect(formatUSD('0')).toBe('$0.00')
  })

  it('不把小额夸大：$0.0594 不能显示成 $0.1', () => {
    // 线上真实案例：total_cost=0.05944，旧的 1 位小数实现显示成 $0.1，
    // 比实际虚高近 70%，对账页面上会被当成多算钱。
    expect(formatUSD('0.0594400000')).toBe('$0.06')
    expect(formatUSD('0.049')).toBe('$0.05')
  })

  it('非零但会被收敛成 0.00 时放宽位数，不显示成零', () => {
    // 显示 $0.00 会让供号商以为账号完全没跑量。
    expect(formatUSD('0.0001')).toBe('$0.0001')
    expect(formatUSD('0.000001')).toBe('$0.000001')
    expect(formatUSD('-0.0001')).toBe('$-0.0001')
  })

  it('接受 number 入参以兼容异常下发', () => {
    expect(formatUSD(12.34)).toBe('$12.34')
  })
})

describe('formatUSDValue / formatUSDExact', () => {
  it('formatUSDValue 不带货币符号', () => {
    expect(formatUSDValue('12.34')).toBe('12.34')
  })

  it('formatUSDExact 输出完整精度，用于导出', () => {
    expect(formatUSDExact('1.2345678901')).toBe('1.2345678901')
    // 导出永远是原值，不受展示位数影响。
    expect(formatUSDExact('0.0594400000')).toBe('0.05944')
  })
})

describe('formatCount', () => {
  it('千分位', () => {
    expect(formatCount(1234567)).toBe('1,234,567')
    expect(formatCount(null)).toBe('0')
  })
})

describe('csvCell', () => {
  it('转义会被表格当公式执行的首字符', () => {
    // 账号名由供号商自填，是注入入口。
    expect(csvCell('=HYPERLINK("http://evil","x")')).toBe("'=HYPERLINK(\"http://evil\",\"x\")")
    expect(csvCell('+1234')).toBe("'+1234")
    expect(csvCell('-1+1')).toBe("'-1+1")
    expect(csvCell('@SUM(A1)')).toBe("'@SUM(A1)")
  })

  it('普通文本原样返回', () => {
    expect(csvCell('my-account')).toBe('my-account')
    expect(csvCell('')).toBe('')
  })
})

describe('amountsMatch', () => {
  it('精确比较，不用容差掩盖口径差异', () => {
    expect(amountsMatch('1.0000000001', '1.0000000001')).toBe(true)
    // 只差 1e-10 也必须判为不一致：两边同源，任何差异都是真实的取数问题。
    expect(amountsMatch('1.0000000001', '1.0000000002')).toBe(false)
  })
})

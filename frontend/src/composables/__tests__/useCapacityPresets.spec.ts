import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getCapacityPresets } = vi.hoisted(() => ({ getCapacityPresets: vi.fn() }))

vi.mock('@/api/admin', () => ({
  adminAPI: { settings: { getCapacityPresets } }
}))

import {
  ensureLoaded,
  resetCapacityPresets,
  setPresets,
  useCapacityPresets
} from '../useCapacityPresets'

const SEED = {
  presets: [
    { key: '1', label: '入门', concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20, adaptive_throttle: true },
    { key: '3', label: '均衡', concurrency: 3, max_sessions: 3, base_rpm: 30, window_cost_limit: 60, adaptive_throttle: true }
  ],
  default_key: '3'
}

describe('useCapacityPresets（模块级单例，失败必须降级）', () => {
  beforeEach(() => {
    resetCapacityPresets()
    getCapacityPresets.mockReset()
  })

  it('加载成功：暴露模板与默认档位，ready 为 true', async () => {
    getCapacityPresets.mockResolvedValue(SEED)
    const cache = useCapacityPresets()
    expect(cache.ready.value).toBe(false)

    await ensureLoaded()

    expect(cache.ready.value).toBe(true)
    expect(cache.failed.value).toBe(false)
    expect(cache.defaultKey.value).toBe('3')
    expect(cache.presets.value.map((p) => p.key)).toEqual(['1', '3'])
    expect(cache.presets.value[1]).toEqual(SEED.presets[1])
  })

  it('并发调用共用同一个请求，加载完成后不再重复请求', async () => {
    getCapacityPresets.mockResolvedValue(SEED)

    await Promise.all([ensureLoaded(), ensureLoaded(), useCapacityPresets().ensureLoaded()])
    await ensureLoaded()

    expect(getCapacityPresets).toHaveBeenCalledTimes(1)
  })

  it('请求失败：不抛异常，状态 failed、模板为空、ready 为 false（调用方据此不显示档位标签）', async () => {
    getCapacityPresets.mockRejectedValue(new Error('boom'))
    const cache = useCapacityPresets()

    await expect(ensureLoaded()).resolves.toBeUndefined()

    expect(cache.failed.value).toBe(true)
    expect(cache.ready.value).toBe(false)
    expect(cache.presets.value).toEqual([])
    expect(cache.defaultKey.value).toBe('')
  })

  it('失败后下一次调用会重试，并能恢复', async () => {
    getCapacityPresets.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(SEED)
    const cache = useCapacityPresets()

    await ensureLoaded()
    expect(cache.failed.value).toBe(true)

    await ensureLoaded()
    expect(getCapacityPresets).toHaveBeenCalledTimes(2)
    expect(cache.failed.value).toBe(false)
    expect(cache.ready.value).toBe(true)
  })

  it('接口方法同步抛错（例如编辑 / 批量弹窗 spec 的 mock 里没有这个方法）同样降级，且能再次重试', async () => {
    getCapacityPresets.mockImplementation(() => {
      throw new TypeError('getCapacityPresets is not a function')
    })
    const cache = useCapacityPresets()

    await expect(ensureLoaded()).resolves.toBeUndefined()
    expect(cache.failed.value).toBe(true)

    // 同步抛错不能把 inflight 卡死：下一次调用必须真的再发一次请求
    getCapacityPresets.mockReset().mockResolvedValue(SEED)
    await ensureLoaded()
    expect(getCapacityPresets).toHaveBeenCalledTimes(1)
    expect(cache.ready.value).toBe(true)
  })

  it('返回内容不合法（没有 presets 数组）按失败处理', async () => {
    getCapacityPresets.mockResolvedValue({ default_key: '3' })
    const cache = useCapacityPresets()

    await ensureLoaded()

    expect(cache.failed.value).toBe(true)
    expect(cache.ready.value).toBe(false)
  })

  it('加载成功但模板列表为空：不算 ready，避免把「没有模板」误判成「自定义」', async () => {
    getCapacityPresets.mockResolvedValue({ presets: [], default_key: '' })
    const cache = useCapacityPresets()

    await ensureLoaded()

    expect(cache.failed.value).toBe(false)
    expect(cache.ready.value).toBe(false)
  })

  it('规整脏数据：数字字段缺失按 0，label 缺失用 key，adaptive 只认严格 true', async () => {
    getCapacityPresets.mockResolvedValue({
      presets: [{ key: 7, label: '', concurrency: '4', adaptive_throttle: 'true' }],
      default_key: '7'
    })
    const cache = useCapacityPresets()

    await ensureLoaded()

    expect(cache.presets.value).toEqual([
      { key: '7', label: '7', concurrency: 4, max_sessions: 0, base_rpm: 0, window_cost_limit: 0, adaptive_throttle: false }
    ])
  })

  it('setPresets 用保存后的内容替换缓存，其它读取方自动刷新', async () => {
    getCapacityPresets.mockResolvedValue(SEED)
    const cache = useCapacityPresets()
    await ensureLoaded()

    setPresets({
      presets: [{ key: '9', label: '新档', concurrency: 9, max_sessions: 9, base_rpm: 90, window_cost_limit: 0, adaptive_throttle: false }],
      default_key: '9'
    })

    expect(cache.presets.value.map((p) => p.key)).toEqual(['9'])
    expect(cache.defaultKey.value).toBe('9')
    expect(cache.ready.value).toBe(true)
    // 已经是 ready，不会再发请求把刚保存的内容覆盖掉
    await ensureLoaded()
    expect(getCapacityPresets).toHaveBeenCalledTimes(1)
  })

  it('setPresets 也能让之前加载失败的缓存直接变为可用', () => {
    const cache = useCapacityPresets()
    setPresets(SEED)
    expect(cache.ready.value).toBe(true)
    expect(cache.failed.value).toBe(false)
  })
})

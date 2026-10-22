import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosInstance } from 'axios'

vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN',
}))

/**
 * 「应用到存量」的前后端契约：
 *
 *   GET  /admin/provider-settings/tiers/:tier/apply-preview   只读预览
 *   POST /admin/provider-settings/tiers/:tier/apply-existing  带 { expected_changed }，
 *                                                             与实际将改动数不一致 → HTTP 409
 *
 * ProviderSettingsPanel.spec 里是直接 mock 掉 adminAPI.providers 的，请求路径、请求体、
 * 以及拦截器对 409 产出的错误形状它都发现不了，所以这一层单独用真实的 apiClient（只替换 adapter）钉住。
 * 面板正是靠这里的 status === 409 来识别「预览之后数据又变了」并自动重新预览。
 */
describe('admin providers API：应用到存量（真实 apiClient，只替换 adapter）', () => {
  let apiClient: AxiosInstance
  let providersAPI: typeof import('@/api/admin/providers').default

  const ok = (data: unknown) =>
    vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, message: 'success', data },
      headers: {},
      config: {},
      statusText: 'OK',
    })

  beforeEach(async () => {
    localStorage.clear()
    window.history.replaceState({}, '', '/')
    vi.resetModules()
    apiClient = (await import('@/api/client')).apiClient
    providersAPI = (await import('@/api/admin/providers')).default
  })

  it('预览：GET .../tiers/:tier/apply-preview，返回契约里的 matched / will_change / samples', async () => {
    const payload = {
      tier: '3',
      matched: 12,
      will_change: 5,
      samples: [
        {
          account_id: 101,
          name: 'alice@example.com',
          changes: [
            { field: 'concurrency', from: 10, to: 3 },
            { field: 'adaptive_throttle', from: false, to: true },
          ],
        },
      ],
    }
    const adapter = ok(payload)
    apiClient.defaults.adapter = adapter

    await expect(providersAPI.getTierApplyPreview('3')).resolves.toEqual(payload)

    expect(adapter).toHaveBeenCalledTimes(1)
    const config = adapter.mock.calls[0][0]
    expect(config.method).toBe('get')
    expect(config.url).toBe('/admin/provider-settings/tiers/3/apply-preview')
  })

  it('预览：模板 key 里的特殊字符会被转义，不会改变路径层级', async () => {
    const adapter = ok({ tier: 'a/b', matched: 0, will_change: 0, samples: [] })
    apiClient.defaults.adapter = adapter

    await providersAPI.getTierApplyPreview('a/b')

    expect(adapter.mock.calls[0][0].url).toBe('/admin/provider-settings/tiers/a%2Fb/apply-preview')
  })

  it('预览：后端把空的 samples 序列化成 null 时统一成空数组', async () => {
    apiClient.defaults.adapter = ok({ tier: '3', matched: 12, will_change: 0, samples: null })

    await expect(providersAPI.getTierApplyPreview('3')).resolves.toMatchObject({
      matched: 12,
      will_change: 0,
      samples: [],
    })
  })

  it('应用：POST .../tiers/:tier/apply-existing，请求体是 { expected_changed }，返回 matched / updated', async () => {
    const adapter = ok({ matched: 12, updated: 5 })
    apiClient.defaults.adapter = adapter

    await expect(providersAPI.applyTierToExisting('3', 5)).resolves.toEqual({ matched: 12, updated: 5 })

    expect(adapter).toHaveBeenCalledTimes(1)
    const config = adapter.mock.calls[0][0]
    expect(config.method).toBe('post')
    expect(config.url).toBe('/admin/provider-settings/tiers/3/apply-existing')
    expect(JSON.parse(config.data)).toEqual({ expected_changed: 5 })
  })

  it('应用：expected_changed 为 0 时也照常发送，不会被当成「没传」丢掉', async () => {
    const adapter = ok({ matched: 12, updated: 0 })
    apiClient.defaults.adapter = adapter

    await providersAPI.applyTierToExisting('3', 0)

    expect(JSON.parse(adapter.mock.calls[0][0].data)).toEqual({ expected_changed: 0 })
  })

  it('应用返回 HTTP 409（PRESET_APPLY_CONFLICT）：经过真实拦截器后以 status 409 被拒绝，面板据此重新预览', async () => {
    apiClient.defaults.adapter = vi.fn().mockRejectedValue({
      response: {
        status: 409,
        data: { code: 409, message: 'preset apply conflict', reason: 'PRESET_APPLY_CONFLICT' },
      },
      config: { url: '/admin/provider-settings/tiers/3/apply-existing', headers: {} },
      code: 'ERR_BAD_REQUEST',
    })

    const error = await providersAPI.applyTierToExisting('3', 5).catch((e: unknown) => e)

    expect(error).toMatchObject({
      status: 409,
      reason: 'PRESET_APPLY_CONFLICT',
      message: 'preset apply conflict',
    })
  })

  it('应用返回其它错误码时不会被当成 409', async () => {
    apiClient.defaults.adapter = vi.fn().mockRejectedValue({
      response: { status: 500, data: { code: 500, message: 'boom' } },
      config: { url: '/admin/provider-settings/tiers/3/apply-existing', headers: {} },
      code: 'ERR_BAD_RESPONSE',
    })

    const error = await providersAPI.applyTierToExisting('3', 5).catch((e: unknown) => e)

    expect(error).toMatchObject({ status: 500, message: 'boom' })
    expect((error as { status: number }).status).not.toBe(409)
  })

  it('档位相关的接口只有「预览」和「应用」两个', () => {
    const tierFunctions = Object.keys(providersAPI).filter((name) => /tier/i.test(name))

    expect(tierFunctions.sort()).toEqual(['applyTierToExisting', 'getTierApplyPreview'])
  })
})

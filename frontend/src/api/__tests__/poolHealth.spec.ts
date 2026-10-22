import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosInstance } from 'axios'

vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN',
}))

/**
 * 公开号池监控接口与页面之间的契约：后台关闭「公开号池监控」后接口返回 HTTP 404，
 * 经过真实的 apiClient 响应拦截器之后，getPoolHealth 必须以 `status === 404` 被拒绝，
 * PoolHealthView 才能据此显示「该页面未开放」。
 *
 * PoolHealthView.spec 里是直接 mock 掉 getPoolHealth 的，拦截器哪天改了错误形状它发现不了，
 * 所以这一层单独用真实的 apiClient（只替换 adapter）钉住。
 */
describe('getPoolHealth（真实 apiClient，只替换 adapter）', () => {
  let apiClient: AxiosInstance
  let getPoolHealth: typeof import('@/api/poolHealth').getPoolHealth

  beforeEach(async () => {
    localStorage.clear()
    window.history.replaceState({}, '', '/')
    vi.resetModules()
    apiClient = (await import('@/api/client')).apiClient
    getPoolHealth = (await import('@/api/poolHealth')).getPoolHealth
  })

  it('接口返回 404（功能未开放）：以 status 404 被拒绝', async () => {
    apiClient.defaults.adapter = vi.fn().mockRejectedValue({
      response: { status: 404, data: { code: 404, message: 'Not found' } },
      config: { url: '/public/pool-health', headers: {} },
      code: 'ERR_BAD_REQUEST',
    })

    await expect(getPoolHealth()).rejects.toMatchObject({ status: 404 })
  })

  it('其它状态码不会被当成 404', async () => {
    apiClient.defaults.adapter = vi.fn().mockRejectedValue({
      response: { status: 500, data: { code: 500, message: 'boom' } },
      config: { url: '/public/pool-health', headers: {} },
      code: 'ERR_BAD_RESPONSE',
    })

    const error = await getPoolHealth().catch((e: unknown) => e)
    expect(error).toMatchObject({ status: 500, message: 'boom' })
  })

  it('开启时返回快照，其中带有实际统计的 group_name', async () => {
    const snapshot = { updated_at: '2026-10-22T00:00:00Z', platform: 'anthropic', group_name: 'team-a' }
    apiClient.defaults.adapter = vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, message: 'success', data: snapshot },
      headers: {},
      config: {},
      statusText: 'OK',
    })

    await expect(getPoolHealth()).resolves.toMatchObject({ group_name: 'team-a' })
  })

  it('请求的是公开接口 /public/pool-health，horizon 缺省 4h，也可以指定', async () => {
    const adapter = vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, data: {} },
      headers: {},
      config: {},
      statusText: 'OK',
    })
    apiClient.defaults.adapter = adapter

    await getPoolHealth()
    await getPoolHealth({ horizon: '1h' })

    expect(adapter.mock.calls[0][0].url).toBe('/public/pool-health')
    expect(adapter.mock.calls[0][0].params.horizon).toBe('4h')
    expect(adapter.mock.calls[1][0].params.horizon).toBe('1h')
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
  },
}))

import { claudePoolAPI } from '@/api/claudePool'

describe('claude pool api', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: {} })
  })

  it('loads the public Claude pool status endpoint', async () => {
    await claudePoolAPI.getStatus()

    expect(get).toHaveBeenCalledWith('/claude-pool/status')
  })
})

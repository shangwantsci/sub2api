import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import {
  useDeploymentLicenseStore,
  CAPABILITY_CHROME_COOKIE_AUTH
} from '@/stores/deploymentLicense'
import adminDeploymentLicenseAPI, {
  type DeploymentLicenseStatus
} from '@/api/admin/deploymentLicense'

vi.mock('@/api/admin/deploymentLicense', () => ({
  default: { getStatus: vi.fn(), refresh: vi.fn() }
}))

const mockedAPI = vi.mocked(adminDeploymentLicenseAPI)

function status(overrides: Partial<DeploymentLicenseStatus> = {}): DeploymentLicenseStatus {
  return { enabled: true, status: 'active', ...overrides }
}

describe('deploymentLicense store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  // The operator's own install must never lose features because the status
  // request has not landed yet.
  it('allows the capability before any fetch', () => {
    const store = useDeploymentLicenseStore()
    expect(store.canUseChromeCookieAuth).toBe(true)
    expect(store.initialized).toBe(false)
  })

  it('allows the capability on an unlicensed community deployment', async () => {
    mockedAPI.getStatus.mockResolvedValue(
      status({
        enabled: false,
        status: 'disabled',
        capabilities: { [CAPABILITY_CHROME_COOKIE_AUTH]: true }
      })
    )
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.canUseChromeCookieAuth).toBe(true)
    expect(store.initialized).toBe(true)
  })

  it('hides the capability when the lease withholds it', async () => {
    mockedAPI.getStatus.mockResolvedValue(
      status({
        managed_customer_id: 'customer-a',
        features: ['gateway'],
        capabilities: { [CAPABILITY_CHROME_COOKIE_AUTH]: false }
      })
    )
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.canUseChromeCookieAuth).toBe(false)
  })

  it('shows the capability when the lease grants it', async () => {
    mockedAPI.getStatus.mockResolvedValue(
      status({
        managed_customer_id: 'customer-a',
        features: ['gateway', CAPABILITY_CHROME_COOKIE_AUTH],
        capabilities: { [CAPABILITY_CHROME_COOKIE_AUTH]: true }
      })
    )
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.canUseChromeCookieAuth).toBe(true)
  })

  // A backend that predates the capabilities field must not blank the UI.
  it('allows the capability when the payload omits capabilities', async () => {
    mockedAPI.getStatus.mockResolvedValue(status())
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.canUseChromeCookieAuth).toBe(true)
  })

  it('keeps the capability available when the request fails', async () => {
    mockedAPI.getStatus.mockRejectedValue(new Error('network down'))
    const store = useDeploymentLicenseStore()
    await expect(store.fetchStatus()).rejects.toThrow('network down')
    expect(store.canUseChromeCookieAuth).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('treats unknown capability names as available', async () => {
    mockedAPI.getStatus.mockResolvedValue(
      status({ capabilities: { [CAPABILITY_CHROME_COOKIE_AUTH]: false } })
    )
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.hasCapability('some_future_feature')).toBe(true)
  })

  it('resets back to unrestricted on logout', async () => {
    mockedAPI.getStatus.mockResolvedValue(
      status({ capabilities: { [CAPABILITY_CHROME_COOKIE_AUTH]: false } })
    )
    const store = useDeploymentLicenseStore()
    await store.fetchStatus()
    expect(store.canUseChromeCookieAuth).toBe(false)
    store.reset()
    expect(store.status).toBeNull()
    expect(store.initialized).toBe(false)
    expect(store.canUseChromeCookieAuth).toBe(true)
  })
})

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import adminDeploymentLicenseAPI, { type DeploymentLicenseStatus } from '@/api/admin/deploymentLicense'

/** Must match service.CapabilityChromeCookieAuth in the backend. */
export const CAPABILITY_CHROME_COOKIE_AUTH = 'chrome_cookie_auth'

export const useDeploymentLicenseStore = defineStore('deploymentLicense', () => {
  const status = ref<DeploymentLicenseStatus | null>(null)
  const loading = ref(false)
  const initialized = ref(false)

  /**
   * Capabilities are opt-out: anything the backend has not explicitly reported
   * as false stays available. An unlicensed community deployment reports every
   * capability as true, and a failed or pending fetch must not hide features
   * from the operator's own install - the backend refuses the call either way.
   */
  function hasCapability(name: string): boolean {
    return status.value?.capabilities?.[name] !== false
  }

  const canUseChromeCookieAuth = computed(() => hasCapability(CAPABILITY_CHROME_COOKIE_AUTH))

  async function fetchStatus(): Promise<DeploymentLicenseStatus> {
    loading.value = true
    try {
      const next = await adminDeploymentLicenseAPI.getStatus()
      status.value = next
      initialized.value = true
      return next
    } finally {
      loading.value = false
    }
  }

  function reset(): void {
    status.value = null
    loading.value = false
    initialized.value = false
  }

  return {
    status,
    loading,
    initialized,
    hasCapability,
    canUseChromeCookieAuth,
    fetchStatus,
    reset
  }
})

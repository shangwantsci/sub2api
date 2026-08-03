import { apiClient } from '@/api/client'

/**
 * Deployment license status for customer-managed installations.
 *
 * The endpoint returns the snapshot directly (no { code, data } envelope), so
 * the response interceptor passes it through untouched.
 */
export interface DeploymentLicenseStatus {
  enabled: boolean
  status: string
  /** Managed capability name -> whether this deployment holds it. */
  capabilities?: Record<string, boolean>
  managed_customer_id?: string
  build_version?: string
  build_commit?: string
  image_digest?: string
  customer_id?: string
  instance_id?: string
  machine_hash_prefix?: string
  features?: string[]
  max_accounts?: number
  max_users?: number
  account_count?: number
  user_count?: number
  issued_at?: string
  expires_at?: string
  grace_ends_at?: string
  last_attempt_at?: string
  last_success_at?: string
  last_error?: string
  host_signal_count?: number
}

export const adminDeploymentLicenseAPI = {
  async getStatus(): Promise<DeploymentLicenseStatus> {
    const { data } = await apiClient.get<DeploymentLicenseStatus>('/admin/deployment-license/status')
    return data
  },

  async refresh(): Promise<DeploymentLicenseStatus> {
    const { data } = await apiClient.post<DeploymentLicenseStatus>('/admin/deployment-license/refresh')
    return data
  }
}

export default adminDeploymentLicenseAPI

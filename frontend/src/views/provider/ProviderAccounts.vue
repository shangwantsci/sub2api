<template>
  <ProviderLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
            {{ t('provider.accounts.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ t('provider.accounts.periodSince') }} {{ formatDateTime(periodStart) }}
          </p>
        </div>
        <RouterLink to="/provider/onboard" class="btn-primary">
          {{ t('provider.accounts.addAccount') }}
        </RouterLink>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <div v-else-if="accounts.length === 0" class="card py-12 text-center">
        <p class="text-gray-500 dark:text-dark-400">{{ t('provider.accounts.empty') }}</p>
      </div>

      <div v-else class="card overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colName') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colStatus') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colHostingType') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colTier') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colRequests') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colCost') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colActions') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <tr v-for="account in accounts" :key="account.id">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900 dark:text-dark-100">{{ account.name }}</div>
                <div v-if="account.notes" class="text-xs text-gray-400">{{ account.notes }}</div>
              </td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium"
                  :class="statusClass(account.status)"
                >
                  {{ t(`provider.accounts.status.${account.status}`) }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ account.hosting_type_label || '-' }}
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ account.tier_label || '-' }}
              </td>
              <td class="px-4 py-3 text-right text-sm text-gray-600 dark:text-dark-300">
                {{ formatCount(account.period_requests) }}
              </td>
              <td class="px-4 py-3 text-right text-sm font-medium text-gray-900 dark:text-dark-100">
                {{ formatUSD(account.period_cost) }}
              </td>
              <td class="px-4 py-3 text-right">
                <div class="flex justify-end gap-2">
                  <button
                    v-if="account.status === 'active'"
                    type="button"
                    class="text-sm text-amber-600 hover:underline dark:text-amber-400"
                    @click="handlePause(account)"
                  >
                    {{ t('provider.accounts.pause') }}
                  </button>
                  <button
                    v-else-if="account.status === 'paused'"
                    type="button"
                    class="text-sm text-emerald-600 hover:underline dark:text-emerald-400"
                    @click="handleResume(account)"
                  >
                    {{ t('provider.accounts.resume') }}
                  </button>
                  <button
                    type="button"
                    class="text-sm text-red-600 hover:underline dark:text-red-400"
                    @click="askOffline(account)"
                  >
                    {{ t('provider.accounts.offline') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
    </div>

    <!-- 下线确认。必须说清楚金额不会因下线而减少，否则供号商会以为下线等于放弃结算。 -->
    <ConfirmDialog
      :show="offlineDialogOpen"
      :title="t('provider.accounts.offlineConfirmTitle')"
      :message="offlineConfirmMessage"
      :confirm-text="t('provider.accounts.offline')"
      danger
      @confirm="handleOffline"
      @cancel="offlineDialogOpen = false"
    />
  </ProviderLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import {
  getCurrentPeriodInfo,
  listAccounts,
  offlineAccount,
  pauseAccount,
  resumeAccount,
} from '@/api/provider'
import type { ProviderAccount } from '@/api/provider'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCount, formatUSD } from '@/utils/providerMoney'

const { t } = useI18n()

const loading = ref(true)
const accounts = ref<ProviderAccount[]>([])
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const periodStart = ref('')
const errorMessage = ref('')
const offlineDialogOpen = ref(false)
const offlineTarget = ref<ProviderAccount | null>(null)

const offlineConfirmMessage = computed(() =>
  t('provider.accounts.offlineConfirmMessage', { name: offlineTarget.value?.name ?? '' })
)

function statusClass(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'paused':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    case 'error':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
  }
}

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

async function load() {
  loading.value = true
  try {
    const [data, info] = await Promise.all([
      listAccounts(pagination.page, pagination.page_size),
      getCurrentPeriodInfo(),
    ])
    accounts.value = data.items
    pagination.total = data.total
    periodStart.value = info.period_start
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.accounts.loadFailed')
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number) {
  pagination.page = page
  void load()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void load()
}


async function handlePause(account: ProviderAccount) {
  try {
    await pauseAccount(account.id)
    await load()
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  }
}

async function handleResume(account: ProviderAccount) {
  try {
    await resumeAccount(account.id)
    await load()
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  }
}

function askOffline(account: ProviderAccount) {
  offlineTarget.value = account
  offlineDialogOpen.value = true
}

async function handleOffline() {
  if (!offlineTarget.value) return
  try {
    await offlineAccount(offlineTarget.value.id)
    await load()
    // 下线的可能是本页最后一条，此时当前页变空。往前退一页重拉，
    // 否则用户会盯着一个空列表以为账号全没了。
    if (accounts.value.length === 0 && pagination.page > 1) {
      pagination.page -= 1
      await load()
    }
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  } finally {
    offlineTarget.value = null
    offlineDialogOpen.value = false
  }
}

onMounted(load)
</script>

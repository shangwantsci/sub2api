<template>
  <ProviderLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
          {{ t('provider.dashboard.title') }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('provider.dashboard.subtitle') }}
        </p>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <!-- 加载失败不能继续渲染金额卡片：period 为 null 时会显示 $0.0，被误读成没跑量。 -->
      <div
        v-else-if="errorMessage"
        class="rounded-lg border border-red-300 bg-red-50 p-6 text-center dark:border-red-800 dark:bg-red-950/40"
      >
        <p class="text-sm font-medium text-red-700 dark:text-red-300">
          {{ t('provider.dashboard.loadFailed') }}
        </p>
        <p class="mt-1 text-xs text-red-600 dark:text-red-400">{{ errorMessage }}</p>
        <button type="button" class="btn-secondary mt-4" @click="reload">
          {{ t('common.retry') }}
        </button>
      </div>

      <template v-else>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('provider.dashboard.accountsTotal') }}
            </p>
            <p class="mt-2 text-3xl font-bold text-gray-900 dark:text-dark-100">
              {{ accountsTotal }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('provider.dashboard.accountsActive') }}
            </p>
            <p class="mt-2 text-3xl font-bold text-emerald-600 dark:text-emerald-400">
              {{ accountsActive }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('provider.dashboard.pendingRequests') }}
            </p>
            <p class="mt-2 text-3xl font-bold text-gray-900 dark:text-dark-100">
              {{ formatCount(period?.totals.requests) }}
            </p>
          </div>
          <div class="card border-indigo-200 p-5 dark:border-indigo-500/30">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('provider.dashboard.pendingCost') }}
            </p>
            <p class="mt-2 text-3xl font-bold text-indigo-600 dark:text-indigo-400">
              {{ formatUSD(period?.totals.standard_cost) }}
            </p>
          </div>
        </div>

        <div class="card p-5">
          <p class="text-sm text-gray-600 dark:text-dark-300">
            {{ t('provider.dashboard.periodSince') }}
            <span class="font-medium">{{ formatDateTime(period?.period_start) }}</span>
          </p>
          <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
            {{ t('provider.settlementTimezone') }}: {{ period?.settlement_timezone }}
          </p>
        </div>

        <div class="flex flex-wrap gap-3">
          <RouterLink to="/provider/onboard" class="btn-primary">
            {{ t('provider.dashboard.goOnboard') }}
          </RouterLink>
          <RouterLink to="/provider/billing" class="btn-secondary">
            {{ t('provider.dashboard.goBilling') }}
          </RouterLink>
        </div>
      </template>
    </div>
  </ProviderLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import { getCurrentBilling, listAccounts } from '@/api/provider'
import type { ProviderAccount, ProviderCurrentPeriod } from '@/api/provider'
import { formatCount, formatUSD } from '@/utils/providerMoney'

const { t } = useI18n()

const loading = ref(true)
const period = ref<ProviderCurrentPeriod | null>(null)
const accounts = ref<ProviderAccount[]>([])
const accountsTotalCount = ref(0)
const errorMessage = ref('')

// 总数取服务端的 total，不用当前页长度——分页后两者不再相等。
const accountsTotal = computed(() => accountsTotalCount.value)
const accountsActive = computed(() => accounts.value.filter((a) => a.status === 'active').length)

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

async function reload() {
  loading.value = true
  errorMessage.value = ''
  try {
    // 概览只展示账号总数与活跃数，取第一页拿 total 即可，不需要全量。
    const [billing, accountList] = await Promise.all([getCurrentBilling(), listAccounts(1, 100)])
    period.value = billing
    accounts.value = accountList.items
    accountsTotalCount.value = accountList.total
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.dashboard.loadFailed')
  } finally {
    loading.value = false
  }
}

onMounted(reload)
</script>

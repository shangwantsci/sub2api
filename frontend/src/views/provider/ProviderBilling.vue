<template>
  <ProviderLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
          {{ t('provider.billing.title') }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('provider.billing.subtitle') }}
        </p>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <!--
        加载失败时只显示错误，不渲染下面的金额区。
        period 为 null 时金额会显示成 $0.0，与「本期确实没跑量」无法区分。
      -->
      <div
        v-else-if="errorMessage && !period"
        class="rounded-lg border border-red-300 bg-red-50 p-6 text-center dark:border-red-800 dark:bg-red-950/40"
      >
        <p class="text-sm font-medium text-red-700 dark:text-red-300">
          {{ t('provider.billing.loadFailed') }}
        </p>
        <p class="mt-1 text-xs text-red-600 dark:text-red-400">{{ errorMessage }}</p>
        <button type="button" class="btn-secondary mt-4" @click="reload">
          {{ t('common.retry') }}
        </button>
      </div>

      <template v-else>
        <!-- 局部失败（例如导出）不遮挡已加载的金额，就地提示即可。 -->
        <div
          v-if="errorMessage"
          class="rounded-md border border-red-300 bg-red-50 px-4 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
        >
          {{ errorMessage }}
        </div>

        <!-- 当前待结算 -->
        <div class="card p-6">
          <div class="flex flex-wrap items-end justify-between gap-4">
            <div>
              <p class="text-sm text-gray-500 dark:text-dark-400">
                {{ t('provider.billing.pendingAmount') }}
              </p>
              <p class="mt-1 text-4xl font-bold text-indigo-600 dark:text-indigo-400">
                {{ formatUSD(period?.totals.standard_cost) }}
              </p>
              <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
                {{ t('provider.billing.periodSince') }} {{ formatDateTime(period?.period_start) }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                {{ t('provider.settlementTimezone') }}: {{ period?.settlement_timezone }}
              </p>
            </div>
            <div class="flex gap-3">
              <button type="button" class="btn-secondary" :disabled="exporting" @click="handleExport">
                {{ exporting ? t('common.loading') : t('provider.billing.exportCsv') }}
              </button>
            </div>
          </div>

          <div class="mt-6 grid gap-4 sm:grid-cols-3">
            <div>
              <p class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('provider.billing.requests') }}
              </p>
              <p class="text-xl font-semibold text-gray-900 dark:text-dark-100">
                {{ formatCount(period?.totals.requests) }}
              </p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('provider.billing.tokens') }}</p>
              <p class="text-xl font-semibold text-gray-900 dark:text-dark-100">
                {{ formatCount(period?.totals.tokens) }}
              </p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('provider.billing.activeAccounts') }}
              </p>
              <p class="text-xl font-semibold text-gray-900 dark:text-dark-100">
                {{ period?.totals.account_count ?? 0 }}
              </p>
            </div>
          </div>
        </div>

        <!-- 分账号明细。已下线账号仍然列出并标注，否则逐行加起来会对不上总额。 -->
        <div class="card overflow-x-auto">
          <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <h2 class="font-semibold text-gray-900 dark:text-dark-100">
              {{ t('provider.billing.byAccount') }}
            </h2>
          </div>
          <table v-if="period?.accounts.length" class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.colAccount') }}
                </th>
                <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.requests') }}
                </th>
                <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.tokens') }}
                </th>
                <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.amount') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="row in period.accounts" :key="row.account_id">
                <td class="px-4 py-3">
                  <span class="text-gray-900 dark:text-dark-100">{{ row.account_name || `#${row.account_id}` }}</span>
                  <span
                    v-if="row.offline"
                    class="ml-2 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-700 dark:text-dark-300"
                  >
                    {{ t('provider.billing.offlineTag') }}
                  </span>
                </td>
                <td class="px-4 py-3 text-right text-sm text-gray-600 dark:text-dark-300">
                  {{ formatCount(row.requests) }}
                </td>
                <td class="px-4 py-3 text-right text-sm text-gray-600 dark:text-dark-300">
                  {{ formatCount(row.tokens) }}
                </td>
                <td class="px-4 py-3 text-right text-sm font-medium text-gray-900 dark:text-dark-100">
                  {{ formatUSD(row.standard_cost) }}
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="px-5 py-8 text-center text-sm text-gray-500 dark:text-dark-400">
            {{ t('provider.billing.noUsage') }}
          </p>
        </div>

        <!-- 历史结算单 -->
        <div class="card overflow-x-auto">
          <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <h2 class="font-semibold text-gray-900 dark:text-dark-100">
              {{ t('provider.billing.history') }}
            </h2>
          </div>
          <table v-if="settlements.length" class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.colPeriod') }}
                </th>
                <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.amount') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.colSettledAt') }}
                </th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                  {{ t('provider.billing.colStatus') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr
                v-for="record in settlements"
                :key="record.id"
                :class="record.status === 'voided' ? 'text-gray-400 line-through dark:text-dark-500' : ''"
              >
                <td class="px-4 py-3 text-sm">
                  {{ formatDate(record.period_start) }} - {{ formatDate(record.period_end) }}
                </td>
                <td class="px-4 py-3 text-right text-sm font-medium">
                  {{ formatUSD(record.standard_cost) }}
                </td>
                <td class="px-4 py-3 text-sm">{{ formatDateTime(record.settled_at) }}</td>
                <td class="px-4 py-3 text-sm">
                  {{ t(`provider.billing.settlementStatus.${record.status}`) }}
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="px-5 py-8 text-center text-sm text-gray-500 dark:text-dark-400">
            {{ t('provider.billing.noSettlements') }}
          </p>
          <div v-if="settlementPage.total > 0" class="border-t border-gray-200 px-5 py-3 dark:border-dark-700">
            <Pagination
              :page="settlementPage.page"
              :total="settlementPage.total"
              :page-size="settlementPage.page_size"
              @update:page="handleSettlementPage"
              @update:pageSize="handleSettlementPageSize"
            />
          </div>
        </div>
      </template>
    </div>
  </ProviderLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import { exportCurrentBilling, getCurrentBilling, listSettlements } from '@/api/provider'
import type { ProviderCurrentPeriod, ProviderSettlement } from '@/api/provider'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCount, formatUSD } from '@/utils/providerMoney'

const { t } = useI18n()

const loading = ref(true)
const exporting = ref(false)
const period = ref<ProviderCurrentPeriod | null>(null)
const settlements = ref<ProviderSettlement[]>([])
const settlementPage = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const errorMessage = ref('')

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}

async function handleExport() {
  exporting.value = true
  errorMessage.value = ''
  try {
    const blob = await exportCurrentBilling()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `pending-settlement-${new Date().toISOString().slice(0, 10)}.csv`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.billing.exportFailed')
  } finally {
    exporting.value = false
  }
}

// 加载失败必须显式报错，绝不能让页面继续渲染金额区。
// period 为 null 时 formatUSD 会返回 $0.0，供号商会把「接口挂了」误读成
// 「这期一分没赚」——对账页面上这是最不能出的错。
async function loadSettlements() {
  const history = await listSettlements(settlementPage.page, settlementPage.page_size)
  settlements.value = history.items
  settlementPage.total = history.total
}

async function reload() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [billing] = await Promise.all([getCurrentBilling(), loadSettlements()])
    period.value = billing
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.billing.loadFailed')
  } finally {
    loading.value = false
  }
}

// 翻页只重拉历史，不动当期金额——当期数据与页码无关，重拉一次纯属浪费。
async function handleSettlementPage(page: number) {
  settlementPage.page = page
  try {
    await loadSettlements()
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.billing.loadFailed')
  }
}

function handleSettlementPageSize(size: number) {
  settlementPage.page_size = size
  settlementPage.page = 1
  void handleSettlementPage(1)
}

onMounted(reload)
</script>

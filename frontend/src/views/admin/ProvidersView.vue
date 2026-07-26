<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
          {{ t('admin.providers.title') }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.providers.subtitle') }}
          <span v-if="summary?.settlement_timezone" class="ml-2 text-xs text-gray-400">
            {{ t('admin.providers.settlementTimezone') }}: {{ summary.settlement_timezone }}
          </span>
        </p>
      </div>

      <!-- 汇总 -->
      <div v-if="summary" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.totalProviders') }}
          </p>
          <p class="mt-2 text-3xl font-bold text-gray-900 dark:text-dark-100">
            {{ summary.total_providers }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.pendingProviders') }}
          </p>
          <p class="mt-2 text-3xl font-bold text-amber-600 dark:text-amber-400">
            {{ summary.pending_providers }}
          </p>
        </div>
        <div class="card border-indigo-200 p-5 dark:border-indigo-500/30">
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.providers.totalPending') }}
          </p>
          <p class="mt-2 text-3xl font-bold text-indigo-600 dark:text-indigo-400">
            {{ formatUSD(summary.total_pending_cost) }}
          </p>
        </div>
        <div class="card flex flex-col justify-center p-5">
          <button
            type="button"
            class="btn-primary"
            :disabled="settleableProviders.length === 0"
            @click="openBatchDialog"
          >
            {{ t('admin.providers.batchSettle') }}
          </button>
          <p class="mt-2 text-xs text-gray-400">
            {{ t('admin.providers.batchSettleHint', { count: settleableProviders.length }) }}
          </p>
        </div>
      </div>

      <div class="flex items-center gap-3">
        <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-dark-300">
          <input v-model="onlyPending" type="checkbox" />
          {{ t('admin.providers.onlyPending') }}
        </label>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <div v-else-if="visibleProviders.length === 0" class="card py-12 text-center">
        <p class="text-gray-500 dark:text-dark-400">{{ t('admin.providers.empty') }}</p>
      </div>

      <div v-else class="card overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colProvider') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colAccounts') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colPeriodStart') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colRequests') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colPending') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colLastSettled') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('admin.providers.colActions') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <tr v-for="row in visibleProviders" :key="row.user_id">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900 dark:text-dark-100">{{ row.email }}</div>
                <div v-if="row.username" class="text-xs text-gray-400">{{ row.username }}</div>
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ row.accounts_active }} / {{ row.accounts_total }}
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ formatDateTime(row.period_start) }}
              </td>
              <td class="px-4 py-3 text-right text-sm text-gray-600 dark:text-dark-300">
                {{ formatCount(row.pending_requests) }}
              </td>
              <td class="px-4 py-3 text-right text-sm font-semibold text-indigo-600 dark:text-indigo-400">
                {{ formatUSD(row.pending_cost) }}
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                <template v-if="row.last_settled_at">
                  {{ formatDateTime(row.last_settled_at) }}
                  <span class="text-xs text-gray-400">({{ formatUSD(row.last_settled_cost) }})</span>
                </template>
                <span v-else class="text-gray-400">-</span>
              </td>
              <td class="px-4 py-3 text-right">
                <div class="flex justify-end gap-3">
                  <button
                    type="button"
                    class="text-sm text-indigo-600 hover:underline dark:text-indigo-400"
                    @click="openDetail(row)"
                  >
                    {{ t('admin.providers.review') }}
                  </button>
                  <button
                    type="button"
                    class="text-sm text-gray-600 hover:underline dark:text-dark-300"
                    @click="openHistory(row)"
                  >
                    {{ t('admin.providers.history') }}
                  </button>
                  <RouterLink
                    :to="{ path: '/admin/accounts', query: { provider_user_id: String(row.user_id) } }"
                    class="text-sm text-gray-600 hover:underline dark:text-dark-300"
                  >
                    {{ t('admin.providers.viewAccounts') }}
                  </RouterLink>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
    </div>

    <!-- 第 1 步：结算前明细。必须先看清分账号清单，不允许一键直接结算。 -->
    <BaseDialog :show="detailOpen" :title="detailTitle" width="wide" @close="detailOpen = false">
      <div v-if="detailLoading" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="detail" class="space-y-4">
        <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <div>
            {{ t('admin.providers.detailPeriod') }}:
            {{ formatDateTime(detail.period_start) }} — {{ formatDateTime(detail.as_of) }}
          </div>
          <div class="mt-1 text-xs text-gray-400">
            {{ t('admin.providers.settlementTimezone') }}: {{ detail.settlement_timezone }}
          </div>
        </div>

        <!-- 一致性自检：明细逐行合计与后端总额对不上说明取数口径有问题，
             此时禁用结算而不是让管理员在两个数字之间猜。 -->
        <div
          v-if="!detailConsistent"
          class="rounded-lg border border-red-300 bg-red-50 p-4 text-sm text-red-700 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-300"
        >
          {{
            t('admin.providers.inconsistentWarning', {
              total: formatUSD(detail.totals.standard_cost),
              sum: formatUSD(detailSum),
            })
          }}
        </div>

        <div class="max-h-80 overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-700">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="sticky top-0 bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providers.colAccount') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-500">
                  {{ t('admin.providers.colRequests') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-500">
                  {{ t('admin.providers.colTokens') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-500">
                  {{ t('admin.providers.colAmount') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="row in detail.accounts" :key="row.account_id">
                <td class="px-3 py-2">
                  {{ row.account_name || `#${row.account_id}` }}
                  <span
                    v-if="row.offline"
                    class="ml-2 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-700"
                  >
                    {{ t('admin.providers.offlineTag') }}
                  </span>
                </td>
                <td class="px-3 py-2 text-right">{{ formatCount(row.requests) }}</td>
                <td class="px-3 py-2 text-right">{{ formatCount(row.tokens) }}</td>
                <td class="px-3 py-2 text-right font-medium">{{ formatUSD(row.standard_cost) }}</td>
              </tr>
            </tbody>
            <tfoot class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <td class="px-3 py-2 font-semibold">{{ t('admin.providers.total') }}</td>
                <td class="px-3 py-2 text-right">{{ formatCount(detail.totals.requests) }}</td>
                <td class="px-3 py-2 text-right">{{ formatCount(detail.totals.tokens) }}</td>
                <td class="px-3 py-2 text-right font-semibold">
                  {{ formatUSD(detail.totals.standard_cost) }}
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn-secondary" @click="detailOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn-secondary"
            :disabled="!detail || detail.accounts.length === 0"
            @click="handleExportPending"
          >
            {{ t('admin.providers.exportDetail') }}
          </button>
          <button
            type="button"
            class="btn-primary"
            :disabled="!detailConsistent || !detailSettleable"
            @click="openSettleConfirm"
          >
            {{ t('admin.providers.settle') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- 第 2 步：结算二次确认 -->
    <BaseDialog
      :show="settleConfirmOpen"
      :title="t('admin.providers.settleConfirmTitle')"
      width="narrow"
      @close="settleConfirmOpen = false"
    >
      <div class="space-y-4">
        <p class="text-center text-4xl font-bold text-indigo-600 dark:text-indigo-400">
          {{ formatUSD(detail?.totals.standard_cost) }}
        </p>
        <p class="text-sm text-gray-600 dark:text-dark-300">
          {{ t('admin.providers.settleConfirmMessage', { provider: activeProvider?.email ?? '' }) }}
        </p>
        <div>
          <label class="input-label" for="settle-notes">{{ t('admin.providers.notes') }}</label>
          <input
            id="settle-notes"
            v-model.trim="settleNotes"
            type="text"
            class="input"
            :placeholder="t('admin.providers.notesPlaceholder')"
          />
        </div>
        <div
          v-if="settleError"
          class="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
        >
          {{ settleError }}
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn-secondary" @click="settleConfirmOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="btn-primary" :disabled="settling" @click="handleSettle">
            {{ settling ? t('common.loading') : t('admin.providers.confirmSettle') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- 批量结算预览 -->
    <BaseDialog
      :show="batchOpen"
      :title="t('admin.providers.batchSettleTitle')"
      width="wide"
      @close="batchOpen = false"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-dark-300">
          {{ t('admin.providers.batchSettleMessage') }}
        </p>
        <div class="max-h-80 overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-700">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="row in settleableProviders" :key="row.user_id">
                <td class="px-3 py-2">
                  <label class="flex items-center gap-2">
                    <input v-model="batchSelected" type="checkbox" :value="row.user_id" />
                    {{ row.email }}
                  </label>
                </td>
                <td class="px-3 py-2 text-right font-medium">{{ formatUSD(row.pending_cost) }}</td>
              </tr>
            </tbody>
            <tfoot class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <td class="px-3 py-2 font-semibold">
                  {{ t('admin.providers.selectedCount', { count: batchSelected.length }) }}
                </td>
                <td class="px-3 py-2 text-right font-semibold">{{ formatUSD(batchTotal) }}</td>
              </tr>
            </tfoot>
          </table>
        </div>
        <div>
          <label class="input-label" for="batch-notes">{{ t('admin.providers.notes') }}</label>
          <input id="batch-notes" v-model.trim="batchNotes" type="text" class="input" />
        </div>
        <!-- 部分失败必须留在弹窗里，关掉就再也看不到是谁没结成。 -->
        <div
          v-if="batchError"
          class="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
        >
          {{ batchError }}
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn-secondary" @click="batchOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn-primary"
            :disabled="batchSelected.length === 0 || settling"
            @click="handleBatchSettle"
          >
            {{ settling ? t('common.loading') : t('admin.providers.confirmSettle') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- 结算历史 -->
    <BaseDialog
      :show="historyOpen"
      :title="historyTitle"
      width="wide"
      @close="historyOpen = false"
    >
      <div v-if="historyLoading" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</div>
      <table v-else-if="history.length" class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
              {{ t('admin.providers.colPeriod') }}
            </th>
            <th class="px-3 py-2 text-right text-xs uppercase text-gray-500">
              {{ t('admin.providers.colAmount') }}
            </th>
            <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
              {{ t('admin.providers.colSettledAt') }}
            </th>
            <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
              {{ t('admin.providers.colStatus') }}
            </th>
            <th class="px-3 py-2 text-right text-xs uppercase text-gray-500">
              {{ t('admin.providers.colActions') }}
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
          <tr
            v-for="(record, index) in history"
            :key="record.id"
            :class="record.status === 'voided' ? 'text-gray-400 line-through dark:text-dark-500' : ''"
          >
            <td class="px-3 py-2">
              {{ formatDate(record.period_start) }} — {{ formatDate(record.period_end) }}
            </td>
            <td class="px-3 py-2 text-right font-medium">{{ formatUSD(record.standard_cost) }}</td>
            <td class="px-3 py-2">{{ formatDateTime(record.settled_at) }}</td>
            <td class="px-3 py-2">{{ t(`admin.providers.status.${record.status}`) }}</td>
            <td class="px-3 py-2 text-right">
              <div class="flex justify-end gap-3">
                <button
                  type="button"
                  class="text-indigo-600 hover:underline dark:text-indigo-400"
                  @click="handleExportSettlement(record.id)"
                >
                  {{ t('admin.providers.export') }}
                </button>
                <!-- 只有最近一条已结算记录可作废：周期是链式的，作废中间某期会让
                     后续周期起点错位，造成金额重复或遗漏。 -->
                <button
                  v-if="index === 0 && record.status === 'settled'"
                  type="button"
                  class="text-red-600 hover:underline dark:text-red-400"
                  @click="openVoid(record)"
                >
                  {{ t('admin.providers.void') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <!-- 加载失败与「确实没有结算记录」必须区分开，否则会被读成这家从没结过账。 -->
      <div
        v-else-if="historyError"
        class="my-4 rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
      >
        {{ historyError }}
      </div>
      <p v-else class="py-8 text-center text-sm text-gray-500">{{ t('admin.providers.noHistory') }}</p>
      <div v-if="historyPage.total > 0" class="mt-4">
        <Pagination
          :page="historyPage.page"
          :total="historyPage.total"
          :page-size="historyPage.page_size"
          @update:page="handleHistoryPage"
          @update:pageSize="handleHistoryPageSize"
        />
      </div>
    </BaseDialog>

    <!-- 作废确认 -->
    <BaseDialog
      :show="voidOpen"
      :title="t('admin.providers.voidTitle')"
      width="narrow"
      @close="voidOpen = false"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-dark-300">
          {{ t('admin.providers.voidMessage', { amount: formatUSD(voidTarget?.standard_cost) }) }}
        </p>
        <div>
          <label class="input-label" for="void-reason">{{ t('admin.providers.voidReason') }}</label>
          <input id="void-reason" v-model.trim="voidReason" type="text" required class="input" />
        </div>
        <div
          v-if="voidError"
          class="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-950/40 dark:text-red-300"
        >
          {{ voidError }}
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn-secondary" @click="voidOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
            :disabled="!voidReason || voiding"
            @click="handleVoid"
          >
            {{ voiding ? t('common.loading') : t('admin.providers.void') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { adminAPI } from '@/api'
import type {
  ProviderCurrentPeriod,
  ProviderSettlement,
  ProviderSummary,
  ProviderSummaryList,
} from '@/api/admin/providers'
import {
  amountsMatch,
  csvCell,
  formatCount,
  formatUSD,
  parseMoney,
  scaledToString,
  sumMoney,
} from '@/utils/providerMoney'
import type { MoneyString } from '@/api/provider/types'

const { t } = useI18n()

const loading = ref(true)
const errorMessage = ref('')
const summary = ref<ProviderSummaryList | null>(null)
const onlyPending = ref(false)

const detailOpen = ref(false)
const detailLoading = ref(false)
const detail = ref<ProviderCurrentPeriod | null>(null)
const activeProvider = ref<ProviderSummary | null>(null)

const settleConfirmOpen = ref(false)
const settleNotes = ref('')
const settling = ref(false)

const batchOpen = ref(false)
const batchSelected = ref<number[]>([])
const batchNotes = ref('')

const historyOpen = ref(false)
const historyLoading = ref(false)
const history = ref<ProviderSettlement[]>([])
const historyPage = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const historyError = ref('')

const voidOpen = ref(false)
const voidTarget = ref<ProviderSettlement | null>(null)
const voidReason = ref('')
const voiding = ref(false)

// 弹窗内的错误单独存：页面级 errorMessage 渲染在主内容底部，
// 弹窗打开时会被遮住，结算失败等于没有任何反馈。
const batchError = ref('')
const settleError = ref('')
const voidError = ref('')

function providerLabel(userID: number): string {
  const row = summary.value?.providers.find((p) => p.user_id === userID)
  return row?.email || `#${userID}`
}

// 金额是十进制字符串，判正负与求和都必须走 providerMoney 的 BigInt 路径，
// 不能直接用 > 0 或 + 累加。
const hasPending = (r: { pending_cost: MoneyString; pending_requests: number }) =>
  parseMoney(r.pending_cost) > 0n || r.pending_requests > 0

const visibleProviders = computed(() => {
  const rows = summary.value?.providers ?? []
  return onlyPending.value ? rows.filter(hasPending) : rows
})

const settleableProviders = computed(
  () => summary.value?.providers.filter(hasPending) ?? []
)

const batchTotal = computed(() =>
  scaledToString(
    sumMoney(
      settleableProviders.value
        .filter((r) => batchSelected.value.includes(r.user_id))
        .map((r) => r.pending_cost)
    )
  )
)

const detailSum = computed(() =>
  scaledToString(sumMoney(detail.value?.accounts.map((r) => r.standard_cost) ?? []))
)

// 明细逐行合计必须与后端总额精确吻合，不吻合就不允许结算。
//
// 两边同源且都是十进制值，用 BigInt 比较不存在浮点误差，
// 任何差异都说明取数口径出了问题，不该被容差掩盖。
const detailConsistent = computed(() => {
  if (!detail.value) return false
  return amountsMatch(detailSum.value, detail.value.totals.standard_cost)
})

// 可结算判定必须与列表的 hasPending 以及后端 periodIsEmpty 保持一致：
// 三者都认「有请求或有金额」。只看金额的话，有请求但金额为 0 的周期
// 在明细页会被禁用按钮卡住，而列表却把它算进待结算，两边对不上。
const detailSettleable = computed(() => {
  const d = detail.value
  if (!d) return false
  return parseMoney(d.totals.standard_cost) > 0n || d.totals.requests > 0
})

const detailTitle = computed(() =>
  t('admin.providers.detailTitle', { provider: activeProvider.value?.email ?? '' })
)
const historyTitle = computed(() =>
  t('admin.providers.historyTitle', { provider: activeProvider.value?.email ?? '' })
)

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

async function load() {
  loading.value = true
  try {
    summary.value = await adminAPI.providers.list()
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('admin.providers.loadFailed')
  } finally {
    loading.value = false
  }
}

async function openDetail(row: ProviderSummary) {
  activeProvider.value = row
  detail.value = null
  detailOpen.value = true
  detailLoading.value = true
  try {
    detail.value = await adminAPI.providers.getCurrentPeriod(row.user_id)
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
    detailOpen.value = false
  } finally {
    detailLoading.value = false
  }
}

function openSettleConfirm() {
  settleNotes.value = ''
  settleError.value = ''
  settleConfirmOpen.value = true
}

async function handleSettle() {
  if (!activeProvider.value) return
  settling.value = true
  settleError.value = ''
  try {
    await adminAPI.providers.settle(activeProvider.value.user_id, settleNotes.value)
    settleConfirmOpen.value = false
    detailOpen.value = false
    await load()
  } catch (error) {
    settleError.value = (error as { message?: string })?.message || ''
  } finally {
    settling.value = false
  }
}

function openBatchDialog() {
  batchSelected.value = settleableProviders.value.map((r) => r.user_id)
  batchNotes.value = ''
  batchError.value = ''
  batchOpen.value = true
}

async function handleBatchSettle() {
  settling.value = true
  batchError.value = ''
  try {
    const result = await adminAPI.providers.settleBatch(batchSelected.value, batchNotes.value)

    // 批量结算是部分成功语义：后端把失败的供号商放进 failed 后继续处理其余的，
    // 整体仍返回 200。不看 failed 就关弹窗的话，管理员会以为全员已结算，
    // 而实际上有人没结——下次结算时那部分金额会被重复计入区间。
    const failedIDs = Object.keys(result.failed ?? {})
    if (failedIDs.length > 0) {
      const detail = failedIDs
        .map((id) => `${providerLabel(Number(id))}: ${result.failed?.[Number(id)] ?? ''}`)
        .join('; ')
      batchError.value = t('admin.providers.batchPartialFailure', {
        settled: result.total_count,
        failed: failedIDs.length,
        detail,
      })
      await load()
      return
    }

    batchOpen.value = false
    await load()
  } catch (error) {
    batchError.value = (error as { message?: string })?.message || ''
  } finally {
    settling.value = false
  }
}

async function openHistory(row: ProviderSummary) {
  activeProvider.value = row
  history.value = []
  historyPage.page = 1
  historyOpen.value = true
  await loadHistory()
}

async function loadHistory() {
  if (!activeProvider.value) return
  historyLoading.value = true
  historyError.value = ''
  try {
    const data = await adminAPI.providers.listSettlements(
      activeProvider.value.user_id,
      historyPage.page,
      historyPage.page_size
    )
    history.value = data.items
    historyPage.total = data.total
  } catch (error) {
    // 失败必须区别于「暂无结算记录」，否则会被误读成这个供号商从没结过账。
    historyError.value = (error as { message?: string })?.message || t('admin.providers.loadFailed')
  } finally {
    historyLoading.value = false
  }
}

function handleHistoryPage(page: number) {
  historyPage.page = page
  void loadHistory()
}

function handleHistoryPageSize(size: number) {
  historyPage.page_size = size
  historyPage.page = 1
  void loadHistory()
}

function openVoid(record: ProviderSettlement) {
  voidTarget.value = record
  voidReason.value = ''
  voidError.value = ''
  voidOpen.value = true
}

async function handleVoid() {
  if (!voidTarget.value) return
  // 后端要求作废必须带原因，前端先拦一道，省得白跑一次请求。
  if (!voidReason.value.trim()) {
    voidError.value = t('admin.providers.voidReasonRequired')
    return
  }
  voiding.value = true
  voidError.value = ''
  try {
    await adminAPI.providers.voidSettlement(voidTarget.value.id, voidReason.value)
    voidOpen.value = false
    historyOpen.value = false
    await load()
  } catch (error) {
    voidError.value = (error as { message?: string })?.message || ''
  } finally {
    voiding.value = false
  }
}

async function handleExportPending() {
  if (!activeProvider.value) return
  const data = await adminAPI.providers.getCurrentPeriod(activeProvider.value.user_id)
  // 当期还没有结算单，前端直接用明细拼 CSV；已结算的走后端导出接口。
  const rows = [
    ['account_id', 'account_name', 'offline', 'requests', 'tokens', 'standard_cost_usd'],
    ...data.accounts.map((a) => [
      String(a.account_id),
      csvCell(a.account_name),
      String(a.offline),
      String(a.requests),
      String(a.tokens),
      // 完整精度原值，不做展示层的 1 位小数收敛。
      String(a.standard_cost),
    ]),
  ]
  const csv = '\uFEFF' + rows.map((r) => r.map((c) => `"${c.replace(/"/g, '""')}"`).join(',')).join('\n')
  downloadBlob(
    new Blob([csv], { type: 'text/csv;charset=utf-8' }),
    `provider-${activeProvider.value.user_id}-pending.csv`
  )
}

async function handleExportSettlement(id: number) {
  const blob = await adminAPI.providers.exportSettlement(id)
  downloadBlob(blob, `settlement-${id}.csv`)
}

onMounted(load)
</script>
